package sequenceplayback

import (
	"context"
	"fmt"
	"sort"
	"time"

	"go.viam.com/rdk/app"
)

// Sequence is one captured demonstration, ready to replay. Joint values are kept in the
// captured unit (degrees); conversion to the arm's inputs happens at play time.
type Sequence struct {
	ID               string
	Offsets          []time.Duration // per frame, from the first arm row
	JointsDeg        [][]float64
	GripperOffsets   []time.Duration // same origin as Offsets; may be negative
	GripperPositions []float64
}

func (s *Sequence) HasGripper() bool        { return len(s.GripperPositions) > 0 }
func (s *Sequence) Duration() time.Duration { return s.Offsets[len(s.Offsets)-1] }

func sortRows(rows []*app.ExportTabularDataResponse) {
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].TimeCaptured.Before(rows[j].TimeCaptured) })
}

func jointValues(payload map[string]any) ([]float64, error) {
	pos, _ := payload["positions"].(map[string]any)
	vals, ok := pos["values"].([]any)
	if !ok {
		return nil, fmt.Errorf("payload has no positions.values list")
	}
	out := make([]float64, len(vals))
	for i, v := range vals {
		f, ok := v.(float64)
		if !ok {
			return nil, fmt.Errorf("positions.values[%d] is %T, not a number", i, v)
		}
		out[i] = f
	}
	return out, nil
}

func gripperValue(payload map[string]any, key string) (float64, error) {
	out, _ := payload["docommand_output"].(map[string]any)
	v, ok := out[key]
	if !ok {
		return 0, fmt.Errorf("docommand_output missing key %q", key)
	}
	f, ok := v.(float64)
	if !ok {
		return 0, fmt.Errorf("docommand_output[%q] is %T, not a number", key, v)
	}
	return f, nil
}

// buildSequence turns exported JointPositions rows (and optional gripper DoCommand rows)
// into a Sequence. Rows may arrive in any order.
func buildSequence(id string, armRows, gripperRows []*app.ExportTabularDataResponse, gripperKey string) (*Sequence, error) {
	if len(armRows) == 0 {
		return nil, fmt.Errorf("sequence %q has no JointPositions rows", id)
	}
	sortRows(armRows)
	sortRows(gripperRows)
	origin := armRows[0].TimeCaptured

	seq := &Sequence{ID: id}
	for i, r := range armRows {
		j, err := jointValues(r.Payload)
		if err != nil {
			return nil, fmt.Errorf("sequence %q arm row %d: %w", id, i, err)
		}
		if i > 0 && len(j) != len(seq.JointsDeg[0]) {
			return nil, fmt.Errorf("sequence %q arm row %d has %d joints, expected %d (joint count changed mid-sequence)", id, i, len(j), len(seq.JointsDeg[0]))
		}
		seq.JointsDeg = append(seq.JointsDeg, j)
		seq.Offsets = append(seq.Offsets, r.TimeCaptured.Sub(origin))
	}
	for i, r := range gripperRows {
		v, err := gripperValue(r.Payload, gripperKey)
		if err != nil {
			return nil, fmt.Errorf("sequence %q gripper row %d: %w", id, i, err)
		}
		seq.GripperPositions = append(seq.GripperPositions, v)
		seq.GripperOffsets = append(seq.GripperOffsets, r.TimeCaptured.Sub(origin))
	}
	return seq, nil
}

// fetchSequence pulls one captured sequence from Viam Cloud.
func fetchSequence(ctx context.Context, dc *app.DataClient, id, sourceArm, sourceGripper, gripperKey string) (*Sequence, error) {
	meta, err := dc.GetSequence(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("getting sequence %q: %w", id, err)
	}
	interval := app.CaptureInterval{Start: meta.StartTime, End: meta.EndTime}
	armRows, err := dc.ExportTabularData(ctx, meta.PartID, sourceArm, "arm", "JointPositions", interval, nil)
	if err != nil {
		return nil, fmt.Errorf("exporting %s JointPositions for sequence %q: %w", sourceArm, id, err)
	}
	var gripperRows []*app.ExportTabularDataResponse
	if sourceGripper != "" {
		gripperRows, err = dc.ExportTabularData(ctx, meta.PartID, sourceGripper, "gripper", "DoCommand", interval, nil)
		if err != nil {
			return nil, fmt.Errorf("exporting %s DoCommand for sequence %q: %w", sourceGripper, id, err)
		}
	}
	return buildSequence(id, armRows, gripperRows, gripperKey)
}

// listSequences summarizes every sequence in a dataset.
func listSequences(ctx context.Context, dc *app.DataClient, datasetID string) ([]map[string]any, error) {
	out := []map[string]any{}
	page, err := dc.SequencesByDatasetID(ctx, datasetID, 100)
	// NextPage hands back an empty page (not nil) once the token runs out.
	for ; err == nil && len(page.Sequences) > 0; page, err = page.NextPage(ctx) {
		for _, s := range page.Sequences {
			out = append(out, map[string]any{
				"id": s.ID, "tags": s.SequenceTags,
				"start": s.StartTime.UTC().Format(time.RFC3339), "duration_s": s.EndTime.Sub(s.StartTime).Seconds(),
			})
		}
	}
	if err != nil {
		return nil, fmt.Errorf("listing sequences in dataset %q: %w", datasetID, err)
	}
	return out, nil
}
