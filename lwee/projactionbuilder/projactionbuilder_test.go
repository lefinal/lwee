package projactionbuilder

import (
	"testing"
)

func Test_validateActionName(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{
		{name: "my-module"},
		{name: "MyModule"},
		{name: "my/module", wantErr: true},
		{name: "", wantErr: true},
		{name: "$what", wantErr: true},
		{name: "my"},
		{name: "https://what", wantErr: true},
		{name: "github.com", wantErr: true},
		{name: "helloWorld"},
		{name: "my-fancy-module"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateActionName(tt.name); (err != nil) != tt.wantErr {
				t.Errorf("validateActionName() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
