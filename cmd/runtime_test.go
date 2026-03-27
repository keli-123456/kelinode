package cmd

import "testing"

func TestParseMemoryLimit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		{name: "bytes", input: "1024", want: 1024},
		{name: "mib", input: "30MiB", want: 30 * 1024 * 1024},
		{name: "mb", input: "512MB", want: 512 * 1000 * 1000},
		{name: "trimmed", input: " 64KiB ", want: 64 * 1024},
		{name: "invalid suffix", input: "12XB", wantErr: true},
		{name: "invalid empty", input: "", wantErr: true},
		{name: "invalid zero", input: "0", wantErr: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseMemoryLimit(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil and %d", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("unexpected result: got %d want %d", got, tc.want)
			}
		})
	}
}
