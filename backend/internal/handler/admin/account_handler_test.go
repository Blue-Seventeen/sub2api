package admin

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShouldRecoverAfterAccountTest(t *testing.T) {
	tests := []struct {
		name     string
		testType string
		want     bool
	}{
		{name: "legacy empty test type", testType: "", want: true},
		{name: "auto keeps legacy recovery", testType: "auto", want: true},
		{name: "text keeps legacy recovery", testType: "text", want: true},
		{name: "case and whitespace normalized", testType: " TEXT ", want: true},
		{name: "explicit asr does not recover runtime state", testType: "asr", want: false},
		{name: "explicit tts does not recover runtime state", testType: "tts", want: false},
		{name: "explicit image does not recover runtime state", testType: "image", want: false},
		{name: "explicit task does not recover runtime state", testType: "task", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, shouldRecoverAfterAccountTest(tt.testType))
		})
	}
}
