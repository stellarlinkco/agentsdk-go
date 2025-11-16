package event

import (
	"errors"
	"fmt"
	"sync"
)

// BusLogger 定义事件总线内部日志接口。
type BusLogger interface {
	Printf(format string, v ...any)
}

// BusOption 为 EventBus 提供可选配置。
type BusOption func(*busConfig)

// EventBus 根据事件类型将消息路由到三个物理通道，并带有缓冲和自动封口能力。
type EventBus struct {
	progress *channelBinding
	control  *channelBinding
	monitor  *channelBinding

	mu       sync.RWMutex
	sealed   bool
	autoSeal map[EventType]struct{}
	logger   BusLogger
}

var (
	errNilBus          = errors.New("event: bus is nil")
	errUnknownEvent    = errors.New("event: unknown type")
	errUnboundProgress = errors.New("event: progress channel not bound")
	errUnboundControl  = errors.New("event: control channel not bound")
	errUnboundMonitor  = errors.New("event: monitor channel not bound")

	// ErrBusSealed 表示事件总线已封口。
	ErrBusSealed = errors.New("event: bus sealed")
)

const defaultBufferSize = 64

type busConfig struct {
	bufferSize int
	autoSeal   map[EventType]struct{}
	logger     BusLogger
}

// NewEventBus 创建解耦事件总线，使用缓冲队列避免直接阻塞生产者。
func NewEventBus(progress, control, monitor chan<- Event, opts ...BusOption) *EventBus {
	cfg := defaultBusConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	return &EventBus{
		progress: newChannelBinding(ChannelProgress, progress, cfg.bufferSize, errUnboundProgress, cfg.logger),
		control:  newChannelBinding(ChannelControl, control, cfg.bufferSize, errUnboundControl, cfg.logger),
		monitor:  newChannelBinding(ChannelMonitor, monitor, cfg.bufferSize, errUnboundMonitor, cfg.logger),
		autoSeal: cfg.autoSeal,
		logger:   cfg.logger,
	}
}

// Emit 根据事件类型映射到对应的通道，具备自动封口能力。
func (b *EventBus) Emit(evt Event) error {
	if b == nil {
		return errNilBus
	}
	normalized := normalizeEvent(evt)
	if err := normalized.Validate(); err != nil {
		return err
	}

	binding, err := b.bindingForType(normalized.Type)
	if err != nil {
		return err
	}

	b.mu.RLock()
	sealed := b.sealed
	b.mu.RUnlock()
	if sealed {
		return ErrBusSealed
	}

	if err := binding.enqueue(normalized); err != nil {
		if b.logger != nil {
			b.logger.Printf("event: drop %s on %s channel: %v", normalized.Type, binding.name, err)
		}
		return err
	}

	if b.shouldAutoSeal(normalized.Type) {
		_ = b.Seal()
	}
	return nil
}

// Seal 手动封口，拒绝新的事件写入。
func (b *EventBus) Seal() error {
	if b == nil {
		return errNilBus
	}
	b.mu.Lock()
	if b.sealed {
		b.mu.Unlock()
		return ErrBusSealed
	}
	b.sealed = true
	b.mu.Unlock()

	b.progress.close()
	b.control.close()
	b.monitor.close()
	return nil
}

// Sealed 返回当前是否已封口。
func (b *EventBus) Sealed() bool {
	if b == nil {
		return true
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.sealed
}

// WithBufferSize 配置内部缓冲长度（>=1）。
func WithBufferSize(size int) BusOption {
	return func(cfg *busConfig) {
		if size < 1 {
			size = 1
		}
		cfg.bufferSize = size
	}
}

// WithLogger 为 EventBus 指定日志记录器。
func WithLogger(l BusLogger) BusOption {
	return func(cfg *busConfig) {
		cfg.logger = l
	}
}

// WithAutoSealTypes 自定义触发自动封口的事件类型集合。
func WithAutoSealTypes(types ...EventType) BusOption {
	return func(cfg *busConfig) {
		cfg.autoSeal = make(map[EventType]struct{}, len(types))
		for _, t := range types {
			cfg.autoSeal[t] = struct{}{}
		}
	}
}

func (b *EventBus) bindingForType(t EventType) (*channelBinding, error) {
	ch, ok := channelForType(t)
	if !ok {
		return nil, fmt.Errorf("%w: %s", errUnknownEvent, t)
	}
	switch ch {
	case ChannelProgress:
		return b.progress, nil
	case ChannelControl:
		return b.control, nil
	case ChannelMonitor:
		return b.monitor, nil
	default:
		return nil, fmt.Errorf("%w: %s", errUnknownEvent, t)
	}
}

func (b *EventBus) shouldAutoSeal(t EventType) bool {
	if len(b.autoSeal) == 0 {
		return false
	}
	_, ok := b.autoSeal[t]
	return ok
}

func defaultBusConfig() busConfig {
	return busConfig{
		bufferSize: defaultBufferSize,
		autoSeal: map[EventType]struct{}{
			EventCompletion: {},
			EventError:      {},
		},
	}
}

type channelBinding struct {
	name       Channel
	buffer     chan Event
	sink       chan<- Event
	errUnbound error
	log        BusLogger
	wg         sync.WaitGroup
}

func newChannelBinding(name Channel, sink chan<- Event, bufferSize int, errUnbound error, logger BusLogger) *channelBinding {
	if bufferSize < 1 {
		bufferSize = 1
	}
	cb := &channelBinding{
		name:       name,
		buffer:     make(chan Event, bufferSize),
		sink:       sink,
		errUnbound: errUnbound,
		log:        logger,
	}
	cb.wg.Add(1)
	go cb.forward()
	return cb
}

func (c *channelBinding) enqueue(evt Event) (err error) {
	if c == nil {
		return errors.New("event: channel binding missing")
	}
	if c.sink == nil {
		return c.errUnbound
	}
	defer func() {
		if r := recover(); r != nil {
			err = ErrBusSealed
		}
	}()
	c.buffer <- evt
	return nil
}

func (c *channelBinding) close() {
	if c == nil {
		return
	}
	close(c.buffer)
	c.wg.Wait()
}

func (c *channelBinding) forward() {
	defer c.wg.Done()
	for evt := range c.buffer {
		c.safeSend(evt)
	}
}

func (c *channelBinding) safeSend(evt Event) {
	if c.sink == nil {
		if c.log != nil {
			c.log.Printf("event: %s channel is nil, dropping %s", c.name, evt.Type)
		}
		return
	}
	defer func() {
		if r := recover(); r != nil && c.log != nil {
			c.log.Printf("event: recovered while sending to %s: %v", c.name, r)
		}
	}()
	c.sink <- evt
}
