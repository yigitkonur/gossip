package smoke

import "testing"

func TestRunnerSmoke(t *testing.T) {
	if 2+2 != 4 {
		t.Fatal("go test runner broken")
	}
}
