package cmd

import (
	"testing"
)

func TestParseLLMResponse_ValidJSON(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantRepo   string
		wantPrompt string
	}{
		{
			name:       "repo with prompt",
			raw:        `{"repo": "github.com/mieubrisse/dotfiles", "prompt": "add a test agent"}`,
			wantRepo:   "github.com/mieubrisse/dotfiles",
			wantPrompt: "add a test agent",
		},
		{
			name:       "repo with empty prompt (open/launch request)",
			raw:        `{"repo": "github.com/mieubrisse/todoist-manager", "prompt": ""}`,
			wantRepo:   "github.com/mieubrisse/todoist-manager",
			wantPrompt: "",
		},
		{
			name:       "blank mission with prompt",
			raw:        `{"repo": "", "prompt": "help me write a bash script"}`,
			wantRepo:   "",
			wantPrompt: "help me write a bash script",
		},
		{
			name:       "blank mission no prompt",
			raw:        `{"repo": "", "prompt": ""}`,
			wantRepo:   "",
			wantPrompt: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action, err := parseLLMResponse(tt.raw)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if action.Repo != tt.wantRepo {
				t.Errorf("repo = %q, want %q", action.Repo, tt.wantRepo)
			}
			if action.Prompt != tt.wantPrompt {
				t.Errorf("prompt = %q, want %q", action.Prompt, tt.wantPrompt)
			}
		})
	}
}

func TestParseLLMResponse_MarkdownFences(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantRepo   string
		wantPrompt string
	}{
		{
			name: "json fence",
			raw: "```json\n" +
				`{"repo": "github.com/owner/repo", "prompt": "do something"}` + "\n" +
				"```",
			wantRepo:   "github.com/owner/repo",
			wantPrompt: "do something",
		},
		{
			name: "plain fence",
			raw: "```\n" +
				`{"repo": "github.com/owner/repo", "prompt": ""}` + "\n" +
				"```",
			wantRepo:   "github.com/owner/repo",
			wantPrompt: "",
		},
		{
			name: "fence with surrounding whitespace",
			raw: "\n  ```json\n" +
				`{"repo": "github.com/owner/repo", "prompt": "fix bug"}` + "\n" +
				"```  \n",
			wantRepo:   "github.com/owner/repo",
			wantPrompt: "fix bug",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action, err := parseLLMResponse(tt.raw)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if action.Repo != tt.wantRepo {
				t.Errorf("repo = %q, want %q", action.Repo, tt.wantRepo)
			}
			if action.Prompt != tt.wantPrompt {
				t.Errorf("prompt = %q, want %q", action.Prompt, tt.wantPrompt)
			}
		})
	}
}

func TestParseLLMResponse_Errors(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{
			name: "not JSON",
			raw:  "I'd be happy to help you with that!",
		},
		{
			name: "empty string",
			raw:  "",
		},
		{
			name: "partial JSON",
			raw:  `{"repo": "github.com/owner/repo"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseLLMResponse(tt.raw)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestBuildMissionNewArgs(t *testing.T) {
	tests := []struct {
		name     string
		action   doAction
		wantArgs []string
	}{
		{
			name:     "repo with prompt",
			action:   doAction{Repo: "github.com/mieubrisse/dotfiles", Prompt: "add a test agent"},
			wantArgs: []string{"github.com/mieubrisse/dotfiles", "--prompt", "add a test agent"},
		},
		{
			name:     "repo without prompt (open/launch)",
			action:   doAction{Repo: "github.com/mieubrisse/todoist-manager", Prompt: ""},
			wantArgs: []string{"github.com/mieubrisse/todoist-manager"},
		},
		{
			name:     "blank mission with prompt",
			action:   doAction{Repo: "", Prompt: "help me write a script"},
			wantArgs: []string{"--blank", "--prompt", "help me write a script"},
		},
		{
			name:     "blank mission no prompt",
			action:   doAction{Repo: "", Prompt: ""},
			wantArgs: []string{"--blank"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildMissionNewArgs(&tt.action)
			if len(got) != len(tt.wantArgs) {
				t.Fatalf("args length = %d, want %d\n  got:  %v\n  want: %v", len(got), len(tt.wantArgs), got, tt.wantArgs)
			}
			for i := range got {
				if got[i] != tt.wantArgs[i] {
					t.Errorf("args[%d] = %q, want %q\n  got:  %v\n  want: %v", i, got[i], tt.wantArgs[i], got, tt.wantArgs)
				}
			}
		})
	}
}

func TestShellQuoteArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "no quoting needed",
			args: []string{"github.com/owner/repo", "--prompt", "simple"},
			want: []string{"github.com/owner/repo", "--prompt", "simple"},
		},
		{
			name: "spaces require quoting",
			args: []string{"--prompt", "fix the auth bug"},
			want: []string{"--prompt", "'fix the auth bug'"},
		},
		{
			name: "single quotes are escaped",
			args: []string{"--prompt", "don't break"},
			want: []string{"--prompt", "'don'\"'\"'t break'"},
		},
		{
			name: "empty args",
			args: []string{},
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shellQuoteArgs(tt.args)
			if len(got) != len(tt.want) {
				t.Fatalf("length = %d, want %d\n  got:  %v\n  want: %v", len(got), len(tt.want), got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
