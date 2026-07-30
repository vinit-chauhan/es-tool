package cli

import (
	"strings"
	"testing"
)

func TestCompletionScripts(t *testing.T) {
	tests := []struct {
		shell  string
		marker string
	}{
		{shell: "bash", marker: "complete -F _es_tool_completion es-tool"},
		{shell: "zsh", marker: "compdef _es_tool_completion es-tool"},
		{shell: "fish", marker: "complete -c es-tool"},
	}

	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			script, err := completionScript(tt.shell)
			if err != nil {
				t.Fatalf("completionScript() error = %v", err)
			}
			if !strings.Contains(script, tt.marker) {
				t.Fatalf("%s completion is missing %q", tt.shell, tt.marker)
			}
			for _, command := range completionCommands {
				if !strings.Contains(script, command.name) {
					t.Errorf("%s completion is missing command %q", tt.shell, command.name)
				}
			}
		})
	}
}

func TestCompletionScriptRejectsUnknownShell(t *testing.T) {
	_, err := completionScript("powershell")
	if err == nil || !strings.Contains(err.Error(), "unsupported shell") {
		t.Fatalf("completionScript() error = %v", err)
	}
}

func TestCompletionSpecsCoverCommands(t *testing.T) {
	specs := make(map[string]bool, len(completionCommands))
	for _, command := range completionCommands {
		if specs[command.name] {
			t.Fatalf("duplicate completion spec for %q", command.name)
		}
		specs[command.name] = true
	}
	for name := range commands {
		if !specs[name] {
			t.Errorf("registered command %q has no completion spec", name)
		}
	}
	for _, name := range []string{"repl", "help", "version"} {
		if !specs[name] {
			t.Errorf("meta command %q has no completion spec", name)
		}
	}
}

func TestCompletionIncludesCommandFlags(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		script, err := completionScript(shell)
		if err != nil {
			t.Fatal(err)
		}
		for _, flag := range []string{"q", "size", "sort", "source", "body", "ids-only", "refresh", "index"} {
			if !strings.Contains(script, flag) {
				t.Errorf("%s completion is missing flag %q", shell, flag)
			}
		}
	}
}

func TestFishCompletionAllowsBodyFileValues(t *testing.T) {
	script := fishCompletion()
	want := "complete -c es-tool -n '__fish_es_tool_using_command search' -l body -r"
	if !strings.Contains(script, want) {
		t.Fatalf("fish completion is missing file-valued body option %q", want)
	}
}
