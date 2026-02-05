package mission

import (
	"testing"
)

func TestParseRepoReference(t *testing.T) {
	tests := []struct {
		name         string
		ref          string
		preferSSH    bool
		wantRepoName string
		wantCloneURL string
		wantErr      bool
	}{
		// SSH URLs should always return SSH clone URLs regardless of preferSSH
		{
			name:         "SSH URL git@github.com format",
			ref:          "git@github.com:owner/repo.git",
			preferSSH:    false,
			wantRepoName: "github.com/owner/repo",
			wantCloneURL: "git@github.com:owner/repo.git",
		},
		{
			name:         "SSH URL without .git suffix",
			ref:          "git@github.com:owner/repo",
			preferSSH:    false,
			wantRepoName: "github.com/owner/repo",
			wantCloneURL: "git@github.com:owner/repo.git",
		},
		{
			name:         "SSH URL ssh:// protocol",
			ref:          "ssh://git@github.com/owner/repo.git",
			preferSSH:    false,
			wantRepoName: "github.com/owner/repo",
			wantCloneURL: "git@github.com:owner/repo.git",
		},
		{
			name:         "SSH URL ssh:// without .git suffix",
			ref:          "ssh://git@github.com/owner/repo",
			preferSSH:    false,
			wantRepoName: "github.com/owner/repo",
			wantCloneURL: "git@github.com:owner/repo.git",
		},

		// HTTPS URLs should always return HTTPS clone URLs regardless of preferSSH
		{
			name:         "HTTPS URL",
			ref:          "https://github.com/owner/repo",
			preferSSH:    true,
			wantRepoName: "github.com/owner/repo",
			wantCloneURL: "https://github.com/owner/repo.git",
		},
		{
			name:         "HTTPS URL with .git suffix",
			ref:          "https://github.com/owner/repo.git",
			preferSSH:    true,
			wantRepoName: "github.com/owner/repo",
			wantCloneURL: "https://github.com/owner/repo.git",
		},
		{
			name:         "HTTPS URL with extra path segments",
			ref:          "https://github.com/owner/repo/tree/main/src",
			preferSSH:    true,
			wantRepoName: "github.com/owner/repo",
			wantCloneURL: "https://github.com/owner/repo.git",
		},

		// Shorthand references should respect preferSSH
		{
			name:         "owner/repo shorthand with preferSSH=false",
			ref:          "owner/repo",
			preferSSH:    false,
			wantRepoName: "github.com/owner/repo",
			wantCloneURL: "https://github.com/owner/repo.git",
		},
		{
			name:         "owner/repo shorthand with preferSSH=true",
			ref:          "owner/repo",
			preferSSH:    true,
			wantRepoName: "github.com/owner/repo",
			wantCloneURL: "git@github.com:owner/repo.git",
		},
		{
			name:         "github.com/owner/repo with preferSSH=false",
			ref:          "github.com/owner/repo",
			preferSSH:    false,
			wantRepoName: "github.com/owner/repo",
			wantCloneURL: "https://github.com/owner/repo.git",
		},
		{
			name:         "github.com/owner/repo with preferSSH=true",
			ref:          "github.com/owner/repo",
			preferSSH:    true,
			wantRepoName: "github.com/owner/repo",
			wantCloneURL: "git@github.com:owner/repo.git",
		},

		// Error cases
		{
			name:    "unsupported host",
			ref:     "gitlab.com/owner/repo",
			wantErr: true,
		},
		{
			name:    "invalid format - too many parts",
			ref:     "a/b/c/d",
			wantErr: true,
		},
		{
			name:    "invalid format - single part",
			ref:     "repo",
			wantErr: true,
		},
		{
			name:    "empty owner",
			ref:     "/repo",
			wantErr: true,
		},
		{
			name:    "empty repo",
			ref:     "owner/",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoName, cloneURL, err := ParseRepoReference(tt.ref, tt.preferSSH)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseRepoReference(%q, %v) expected error, got nil", tt.ref, tt.preferSSH)
				}
				return
			}

			if err != nil {
				t.Errorf("ParseRepoReference(%q, %v) unexpected error: %v", tt.ref, tt.preferSSH, err)
				return
			}

			if repoName != tt.wantRepoName {
				t.Errorf("ParseRepoReference(%q, %v) repoName = %q, want %q", tt.ref, tt.preferSSH, repoName, tt.wantRepoName)
			}

			if cloneURL != tt.wantCloneURL {
				t.Errorf("ParseRepoReference(%q, %v) cloneURL = %q, want %q", tt.ref, tt.preferSSH, cloneURL, tt.wantCloneURL)
			}
		})
	}
}
