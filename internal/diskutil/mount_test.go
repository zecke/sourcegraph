package diskutil

import (
	"testing"
)

func TestFindMountPoint(t *testing.T) {
	type args struct {
		d string
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr bool
	}{
		{
			name:    "mount point of root is root",
			args:    args{d: "/"},
			want:    "/",
			wantErr: false,
		},
		// What else can we portably count on?
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FindMountPoint(tt.args.d)
			if (err != nil) != tt.wantErr {
				t.Errorf("FindMountPoint() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("FindMountPoint() = %v, want %v", got, tt.want)
			}
		})
	}
}
