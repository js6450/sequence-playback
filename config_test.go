package sequenceplayback

import (
	"testing"

	"go.viam.com/test"
)

func TestConfigValidate(t *testing.T) {
	t.Run("arm required", func(t *testing.T) {
		_, _, err := (&Config{}).Validate("x")
		test.That(t, err, test.ShouldNotBeNil)
		test.That(t, err.Error(), test.ShouldContainSubstring, "arm")
	})
	t.Run("arm only", func(t *testing.T) {
		deps, _, err := (&Config{Arm: "a"}).Validate("x")
		test.That(t, err, test.ShouldBeNil)
		test.That(t, deps, test.ShouldResemble, []string{"a"})
	})
	t.Run("arm and gripper", func(t *testing.T) {
		deps, _, err := (&Config{Arm: "a", Gripper: "g"}).Validate("x")
		test.That(t, err, test.ShouldBeNil)
		test.That(t, deps, test.ShouldResemble, []string{"a", "g"})
	})
	t.Run("source_gripper without gripper", func(t *testing.T) {
		_, _, err := (&Config{Arm: "a", SourceGripper: "g"}).Validate("x")
		test.That(t, err, test.ShouldNotBeNil)
		test.That(t, err.Error(), test.ShouldContainSubstring, "source_gripper")
	})
}

func TestConfigArmExtraDefault(t *testing.T) {
	test.That(t, (&Config{}).armExtra(), test.ShouldResemble, map[string]any{"wait": false, "streamed": true})
	test.That(t, (&Config{ArmExtra: map[string]any{}}).armExtra(), test.ShouldResemble, map[string]any{})
}
