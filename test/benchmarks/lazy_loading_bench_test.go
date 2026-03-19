package benchmarks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stellarlinkco/agentsdk-go/pkg/api"
	"github.com/stellarlinkco/agentsdk-go/pkg/config"
	"github.com/stellarlinkco/agentsdk-go/pkg/model"
	"github.com/stellarlinkco/agentsdk-go/pkg/runtime/skills"
)

const iterationsPerBenchmark = 100

type noopModel struct{}

func (noopModel) Complete(context.Context, model.Request) (*model.Response, error) {
	return &model.Response{
		Message: model.Message{Role: "assistant"},
	}, nil
}

func (noopModel) CompleteStream(ctx context.Context, req model.Request, cb model.StreamHandler) error {
	resp, err := noopModel{}.Complete(ctx, req)
	if err != nil {
		return err
	}
	if cb != nil {
		return cb(model.StreamResult{Final: true, Response: resp})
	}
	return nil
}

func BenchmarkSkillsLazyLoading(b *testing.B) {
	root := b.TempDir()
	writeSkillFile(b, root, "lazy", "lazy body")

	var reads atomic.Int64
	restore := skills.SetReadFileForTest(func(path string) ([]byte, error) {
		reads.Add(1)
		return os.ReadFile(path)
	})
	b.Cleanup(restore)

	opts := skills.LoaderOptions{
		ProjectRoot: root,
		EnableUser:  false,
	}

	var startupReads int64
	var execReads int64

	b.ReportAllocs()
	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		for i := 0; i < iterationsPerBenchmark; i++ {
			reads.Store(0)

			regs, errs := skills.LoadFromFS(opts)
			if len(errs) != 0 {
				b.Fatalf("skills load: %v", errs)
			}
			if len(regs) == 0 {
				b.Fatal("no skills loaded")
			}

			afterLoad := reads.Load()
			startupReads += afterLoad

			if _, err := regs[0].Handler.Execute(context.Background(), skills.ActivationContext{}); err != nil {
				b.Fatalf("skill execute: %v", err)
			}
			execReads += reads.Load() - afterLoad
		}
	}
	b.StopTimer()

	ops := float64(b.N * iterationsPerBenchmark)
	b.ReportMetric(float64(startupReads)/ops, "startup-read/op")
	b.ReportMetric(float64(execReads)/ops, "body-read/op")
}

func BenchmarkRuntimeStartup(b *testing.B) {
	root := b.TempDir()
	b.Setenv("HOME", root)
	writeSettingsFile(b, root)
	writeSkillFile(b, root, "lazy", "lazy body")

	opts := api.Options{
		EntryPoint:  api.EntryPointCLI,
		ProjectRoot: root,
		Model:       noopModel{},
		SettingsLoader: &config.SettingsLoader{
			ProjectRoot: root,
		},
		Sandbox: api.SandboxOptions{
			Root: root,
		},
	}

	b.ReportAllocs()
	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		for i := 0; i < iterationsPerBenchmark; i++ {
			rt, err := api.New(context.Background(), opts)
			if err != nil {
				b.Fatalf("runtime init: %v", err)
			}
			_ = rt.Close()
		}
	}
}

func writeSkillFile(b *testing.B, root, name, body string) {
	b.Helper()
	path := filepath.Join(root, ".agents", "skills", name, "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		b.Fatalf("mkdir skills: %v", err)
	}
	content := fmt.Sprintf("---\nname: %s\ndescription: %s\n---\n%s", name, name+" skill", body)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		b.Fatalf("write skill: %v", err)
	}
}

func writeSettingsFile(b *testing.B, root string) {
	b.Helper()
	path := filepath.Join(root, ".agents", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		b.Fatalf("mkdir settings: %v", err)
	}
	const settings = `{"sandbox":{"enabled":false}}`
	if err := os.WriteFile(path, []byte(settings), 0o600); err != nil {
		b.Fatalf("write settings: %v", err)
	}
}
