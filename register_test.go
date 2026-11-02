package cursor

import (
	"errors"
	"testing"

	contracts "github.com/Herrscherd/herrscher-contracts"
)

func TestSelfRegisteredAsBackend(t *testing.T) {
	for _, p := range contracts.Default.Backends() {
		if p.Manifest.Kind == "cursor" {
			if p.Backend == nil {
				t.Fatal("registered cursor plugin has a nil backend factory")
			}
			return
		}
	}
	t.Fatal("cursor backend did not self-register into contracts.Default")
}

func TestDeclaresItCannotGate(t *testing.T) {
	for _, p := range contracts.Default.Backends() {
		if p.Manifest.Kind != "cursor" {
			continue
		}
		if p.Manifest.Capabilities.Gate != contracts.GrainNone {
			t.Fatalf("gate = %q, want none", p.Manifest.Capabilities.Gate)
		}
		if p.Manifest.Capabilities.GateWhy == "" {
			t.Fatal("cursor refuses to gate without telling the operator why")
		}
		return
	}
	t.Fatal("cursor backend did not self-register into contracts.Default")
}

func cursorPlugin(t *testing.T) contracts.Plugin {
	t.Helper()
	plugins := contracts.Default.Backends()
	for index := range plugins {
		if plugins[index].Manifest.Kind == "cursor" {
			return plugins[index]
		}
	}
	t.Fatal("cursor backend did not self-register into contracts.Default")
	return contracts.Plugin{}
}

func TestManifestDeclaresEverySettingTheFactoryReads(t *testing.T) {
	declared := make(map[string]bool)
	for _, setting := range cursorPlugin(t).Manifest.Config {
		declared[setting.Key] = true
	}
	for _, key := range []string{"cmd", "model", "stream", "dir", "kind", "resume"} {
		if !declared[key] {
			t.Errorf("setting %q is read by the factory but not declared in the manifest", key)
		}
	}
}

func TestStreamSetting(t *testing.T) {
	for _, tc := range []struct {
		name    string
		value   string
		want    bool
		wantErr bool
	}{
		{name: "absent", value: "", want: true},
		{name: "false", value: "false", want: false},
		{name: "zero", value: "0", want: false},
		{name: "off", value: "off", want: false},
		{name: "upper off", value: "OFF", want: false},
		{name: "upper false", value: "FALSE", want: false},
		{name: "padded false", value: " false ", want: false},
		{name: "true", value: "true", want: true},
		{name: "one", value: "1", want: true},
		{name: "garbage", value: "yes-please", wantErr: true},
		{name: "no is not a value", value: "no", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := contracts.PluginConfig{Settings: map[string]string{"stream": tc.value}}
			got, err := streamSetting(cfg)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidSetting) {
					t.Fatalf("streamSetting(%q) error = %v, want ErrInvalidSetting", tc.value, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("streamSetting(%q): %v", tc.value, err)
			}
			if got != tc.want {
				t.Fatalf("streamSetting(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

func responderKind(backend contracts.Backend) string {
	switch backend.(type) {
	case *streamResponder:
		return "stream"
	case *oneShotResponder:
		return "oneshot"
	}
	return "other"
}

func TestRegisteredFactoryReadsSettings(t *testing.T) {
	plugin := cursorPlugin(t)
	for _, tc := range []struct {
		name          string
		settings      map[string]string
		wantResponder string
		wantErr       error
		wantUnknown   bool
	}{
		{name: "default is stream", settings: make(map[string]string), wantResponder: "stream"},
		{name: "stream off is oneshot", settings: map[string]string{"stream": "off"}, wantResponder: "oneshot"},
		{name: "kind wins over stream", settings: map[string]string{"kind": "stream", "stream": "off"}, wantResponder: "stream"},
		{name: "unreadable stream fails", settings: map[string]string{"stream": "garbage"}, wantErr: ErrInvalidSetting},
		{name: "unknown kind fails", settings: map[string]string{"kind": "batch"}, wantUnknown: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkRegisteredFactory(t, plugin, tc.settings, tc.wantResponder, tc.wantErr, tc.wantUnknown)
		})
	}
}

func checkRegisteredFactory(t *testing.T, plugin contracts.Plugin, settings map[string]string, wantResponder string, wantErr error, wantUnknown bool) {
	t.Helper()
	backend, err := plugin.Backend(t.Context(), contracts.PluginConfig{Settings: settings})
	if wantErr != nil {
		if !errors.Is(err, wantErr) {
			t.Fatalf("factory error = %v, want %v", err, wantErr)
		}
		return
	}
	if wantUnknown {
		if _, ok := errors.AsType[*UnknownBackendKindError](err); !ok {
			t.Fatalf("factory error = %v, want an unknown backend kind error", err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if got := responderKind(backend); got != wantResponder {
		t.Fatalf("responder = %s, want %s", got, wantResponder)
	}
}

func TestRegisteredFactoryRunsTheConfiguredCommand(t *testing.T) {
	stub := writeStubScript(t, `cat > /dev/null
printf '{"type":"result","subtype":"success","result":"from the configured command"}\n'`)
	backend, err := cursorPlugin(t).Backend(t.Context(), contracts.PluginConfig{Settings: map[string]string{
		"cmd":    stub,
		"stream": "off",
	}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := backend.Respond(t.Context(), contracts.Prompt{Content: "hi"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "from the configured command" {
		t.Fatalf("Respond = %q, want the stub's result", got)
	}
}

func TestRegisteredFactoryThreadsTheResumeSetting(t *testing.T) {
	backend, err := cursorPlugin(t).Backend(t.Context(), contracts.PluginConfig{Settings: map[string]string{
		"resume": "sess-5",
	}})
	if err != nil {
		t.Fatal(err)
	}
	resumable, ok := backend.(contracts.ResumeAware)
	if !ok {
		t.Fatalf("backend is %T, want a resume aware backend", backend)
	}
	if got := resumable.ResumeToken(); got != "sess-5" {
		t.Fatalf("ResumeToken = %q, want sess-5", got)
	}
}
