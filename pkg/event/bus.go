package event

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
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

	seq        atomic.Int64
	store      EventStore
	storeAsync bool
}

var (
	errNilBus             = errors.New("event: bus is nil")
	errUnknownEvent       = errors.New("event: unknown type")
	errUnboundProgress    = errors.New("event: progress channel not bound")
	errUnboundControl     = errors.New("event: control channel not bound")
	errUnboundMonitor     = errors.New("event: monitor channel not bound")
	errStoreNotConfigured = errors.New("event: store not configured")

	// ErrBusSealed 表示事件总线已封口。
	ErrBusSealed = errors.New("event: bus sealed")
)

const defaultBufferSize = 64

type busConfig struct {
	bufferSize int
	autoSeal   map[EventType]struct{}
	logger     BusLogger
	store      EventStore
	storeAsync bool
}

// NewEventBus 创建解耦事件总线，使用缓冲队列避免直接阻塞生产者。
func NewEventBus(progress, control, monitor chan<- Event, opts ...BusOption) *EventBus {
	cfg := defaultBusConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	bus := &EventBus{
		progress:   newChannelBinding(ChannelProgress, progress, cfg.bufferSize, errUnboundProgress, cfg.logger),
		control:    newChannelBinding(ChannelControl, control, cfg.bufferSize, errUnboundControl, cfg.logger),
		monitor:    newChannelBinding(ChannelMonitor, monitor, cfg.bufferSize, errUnboundMonitor, cfg.logger),
		autoSeal:   cfg.autoSeal,
		logger:     cfg.logger,
		store:      cfg.store,
		storeAsync: cfg.storeAsync,
	}
	if cfg.store != nil {
		if last, err := cfg.store.LastBookmark(); err == nil && last != nil {
			bus.seq.Store(last.Seq)
		} else if err != nil && cfg.logger != nil {
			cfg.logger.Printf("event: load last bookmark: %v", err)
		}
	}
	return bus
}

// Emit 根据事件类型映射到对应的通道，具备自动封口能力。
func (b *EventBus) Emit(evt Event) error {
	if b == nil {
		return errNilBus
	}
	normalized := normalizeEvent(evt)
	b.assignBookmark(&normalized)
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
	b.persistEvent(normalized)

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

// WithEventStore 配置事件持久化存储。
func WithEventStore(store EventStore) BusOption {
	return func(cfg *busConfig) {
		cfg.store = store
	}
}

// WithAsyncStoreWrites 启用异步持久化，防止阻塞 Emit。
func WithAsyncStoreWrites() BusOption {
	return func(cfg *busConfig) {
		cfg.storeAsync = true
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

// SubscribeSince 返回带有历史回放的进度事件订阅。
func (b *EventBus) SubscribeSince(bookmark *Bookmark) (<-chan Event, error) {
	if b == nil {
		return nil, errNilBus
	}
	if b.store == nil {
		return nil, errStoreNotConfigured
	}
	history, err := b.store.ReadSince(bookmark)
	if err != nil {
		return nil, err
	}
	ch := make(chan Event, len(history)+defaultBufferSize)
	go func() {
		defer close(ch)
		for _, evt := range history {
			ch <- evt
		}
		// Historical replay complete; nothing else to push because live wiring
		// requires higher-level coordination not covered here.
	}()
	return ch, nil
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

func (b *EventBus) assignBookmark(evt *Event) {
	if b == nil || evt == nil || b.store == nil {
		return
	}
	if evt.Bookmark != nil {
		b.observeBookmark(evt.Bookmark)
		return
	}
	seq := b.seq.Add(1)
	evt.Bookmark = newBookmark(seq)
}

func (b *EventBus) observeBookmark(bookmark *Bookmark) {
	if b == nil || bookmark == nil {
		return
	}
	for {
		curr := b.seq.Load()
		if bookmark.Seq <= curr {
			return
		}
		if b.seq.CompareAndSwap(curr, bookmark.Seq) {
			return
		}
	}
}

func (b *EventBus) persistEvent(evt Event) {
	if b == nil || b.store == nil || evt.Bookmark == nil {
		return
	}
	appendFn := func() {
		if err := b.store.Append(evt); err != nil && b.logger != nil {
			b.logger.Printf("event: persist seq=%d failed: %v", evt.Bookmark.Seq, err)
		}
	}
	if b.storeAsync {
		go appendFn()
		return
	}
	appendFn()
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
