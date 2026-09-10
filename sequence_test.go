package sequenceplayback

import (
	"testing"
	"time"

	"go.viam.com/rdk/app"
	"go.viam.com/test"
)

var t0 = time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)

func armRow(at time.Duration, deg ...float64) *app.ExportTabularDataResponse {
	vals := make([]any, len(deg))
	for i, d := range deg {
		vals[i] = d
	}
	return &app.ExportTabularDataResponse{
		TimeCaptured: t0.Add(at),
		Payload:      map[string]any{"positions": map[string]any{"values": vals}},
	}
}

func gripRow(at time.Duration, key string, v any) *app.ExportTabularDataResponse {
	return &app.ExportTabularDataResponse{
		TimeCaptured: t0.Add(at),
		Payload:      map[string]any{"docommand_output": map[string]any{key: v}},
	}
}

func TestBuildSequenceSortsAndOffsetsFromFirstArmRow(t *testing.T) {
	arm := []*app.ExportTabularDataResponse{
		armRow(200*time.Millisecond, 10, 20),
		armRow(0, 1, 2),
		armRow(100*time.Millisecond, 5, 6),
	}
	grip := []*app.ExportTabularDataResponse{
		gripRow(150*time.Millisecond, "position_percentage", 50.0),
		gripRow(-50*time.Millisecond, "position_percentage", 90.0),
	}
	seq, err := buildSequence("abc", arm, grip, "position_percentage")
	test.That(t, err, test.ShouldBeNil)
	test.That(t, seq.ID, test.ShouldEqual, "abc")
	test.That(t, seq.JointsDeg, test.ShouldResemble, [][]float64{{1, 2}, {5, 6}, {10, 20}})
	test.That(t, seq.Offsets, test.ShouldResemble, []time.Duration{0, 100 * time.Millisecond, 200 * time.Millisecond})
	test.That(t, seq.GripperPositions, test.ShouldResemble, []float64{90, 50})
	test.That(t, seq.GripperOffsets, test.ShouldResemble, []time.Duration{-50 * time.Millisecond, 150 * time.Millisecond})
	test.That(t, seq.Duration(), test.ShouldEqual, 200*time.Millisecond)
}

func TestBuildSequenceNoGripperRows(t *testing.T) {
	seq, err := buildSequence("abc", []*app.ExportTabularDataResponse{armRow(0, 1)}, nil, "position_percentage")
	test.That(t, err, test.ShouldBeNil)
	test.That(t, seq.HasGripper(), test.ShouldBeFalse)
}

func TestBuildSequenceErrors(t *testing.T) {
	cases := map[string]struct {
		arm, grip []*app.ExportTabularDataResponse
		want      string
	}{
		"no arm rows":       {nil, nil, "no JointPositions rows"},
		"joint count drift": {[]*app.ExportTabularDataResponse{armRow(0, 1, 2), armRow(time.Second, 1)}, nil, "joint count"},
		"missing key": {[]*app.ExportTabularDataResponse{armRow(0, 1)},
			[]*app.ExportTabularDataResponse{gripRow(0, "other", 1.0)}, `missing key "position_percentage"`},
		"non-numeric": {[]*app.ExportTabularDataResponse{armRow(0, 1)},
			[]*app.ExportTabularDataResponse{gripRow(0, "position_percentage", "open")}, "not a number"},
		"bad arm payload": {[]*app.ExportTabularDataResponse{{TimeCaptured: t0, Payload: map[string]any{"nope": 1}}}, nil, "positions.values"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := buildSequence("abc", c.arm, c.grip, "position_percentage")
			test.That(t, err, test.ShouldNotBeNil)
			test.That(t, err.Error(), test.ShouldContainSubstring, c.want)
		})
	}
}
