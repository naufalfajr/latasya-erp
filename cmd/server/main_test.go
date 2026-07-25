package main

import "testing"

func TestEnvOr(t *testing.T) {
	cases := []struct {
		name     string
		key      string
		envValue string
		setEnv   bool
		fallback string
		want     string
	}{
		{name: "env set", key: "LATASYA_TEST_ENVOR", envValue: "custom", setEnv: true, fallback: "default", want: "custom"},
		{name: "env unset", key: "LATASYA_TEST_ENVOR", setEnv: false, fallback: "default", want: "default"},
		{name: "env empty falls back", key: "LATASYA_TEST_ENVOR", envValue: "", setEnv: true, fallback: "default", want: "default"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.setEnv {
				t.Setenv(c.key, c.envValue)
			}
			if got := envOr(c.key, c.fallback); got != c.want {
				t.Errorf("envOr(%q, %q) = %q, want %q", c.key, c.fallback, got, c.want)
			}
		})
	}
}
