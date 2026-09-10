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

func TestConfigDefaults(t *testing.T) {
	cfg := &Config{Arm: "a", Gripper: "g"}
	test.That(t, cfg.sourceArm(), test.ShouldEqual, "a")
	test.That(t, cfg.sourceGripper(), test.ShouldEqual, "g")
	test.That(t, cfg.gripperKey(), test.ShouldEqual, "position_percentage")
	test.That(t, cfg.armExtra(), test.ShouldResemble, map[string]any{"wait": false, "streamed": true})

	cfg = &Config{Arm: "a", Gripper: "g", SourceArm: "leader", SourceGripper: "lg", GripperPositionKey: "k", ArmExtra: map[string]any{}}
	test.That(t, cfg.sourceArm(), test.ShouldEqual, "leader")
	test.That(t, cfg.sourceGripper(), test.ShouldEqual, "lg")
	test.That(t, cfg.gripperKey(), test.ShouldEqual, "k")
	test.That(t, cfg.armExtra(), test.ShouldResemble, map[string]any{})
}
