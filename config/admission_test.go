package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAdmissionConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     AdmissionConfig
		wantErr string
	}{
		{"disabled skips checks", AdmissionConfig{Enable: false, SlowP50: "bogus"}, ""},
		{"ok", AdmissionConfig{Enable: true, SlowP50: "500ms", MinSamples: 3}, ""},
		{"bad slow_p50", AdmissionConfig{Enable: true, SlowP50: "soon"}, "slow_p50"},
		{"negative min_samples", AdmissionConfig{Enable: true, MinSamples: -1}, "min_samples"},
		{"negative local limit", AdmissionConfig{Enable: true, Local: AdmissionClassConfig{Limit: -1}}, "local.limit"},
		{"bad io wait", AdmissionConfig{Enable: true, IO: AdmissionClassConfig{WaitTimeout: "later"}}, "io.wait_timeout"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}
