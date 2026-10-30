package host

import "testing"

// The password store is keyed by the name the user addressed
// (`config set-sudo-password acchost` writes "acchost/sudo"), while the resolved
// hostname (127.0.0.1) is only a legacy fallback. Key order therefore belongs to
// the Host, not to the store.
func TestCredentialKeysPriority(t *testing.T) {
	tests := []struct {
		name      string
		addressed string
		canonical string
		hostname  string
		want      []string
	}{
		{
			name:      "addressed key first, hostname last",
			addressed: "acchost",
			canonical: "acchost",
			hostname:  "127.0.0.1",
			want:      []string{"acchost", "127.0.0.1"},
		},
		{
			name:      "alias addressed, canonical host name in the middle",
			addressed: "database",
			canonical: "db1",
			hostname:  "10.0.0.5",
			want:      []string{"database", "db1", "10.0.0.5"},
		},
		{
			name:      "direct address key plus parsed hostname",
			addressed: "root@10.0.0.5:2222",
			canonical: "root@10.0.0.5:2222",
			hostname:  "10.0.0.5",
			want:      []string{"root@10.0.0.5:2222", "10.0.0.5"},
		},
		{
			name:      "empty identifiers are dropped",
			addressed: "acchost",
			canonical: "",
			hostname:  "",
			want:      []string{"acchost"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := credentialKeys(tc.addressed, tc.canonical, tc.hostname)
			if len(got) != len(tc.want) {
				t.Fatalf("credentialKeys() = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("credentialKeys() = %v, want %v", got, tc.want)
				}
			}
		})
	}
}
