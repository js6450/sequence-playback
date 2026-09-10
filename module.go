package sequenceplayback

import (
	"context"
	"errors"
	"fmt"
	"sync"

	pb "go.viam.com/api/component/arm/v1"
	"go.viam.com/rdk/app"
	"go.viam.com/rdk/components/arm"
	"go.viam.com/rdk/components/gripper"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/referenceframe"
	"go.viam.com/rdk/resource"
	generic "go.viam.com/rdk/services/generic"
)

var ArmPlayback = resource.NewModel("devrel", "sequence-playback", "arm-playback")

func init() {
	resource.RegisterService(generic.API, ArmPlayback,
		resource.Registration[resource.Resource, *Config]{
			Constructor: NewArmPlayback,
		},
	)
}

type Config struct {
	Arm                string         `json:"arm"`
	Gripper            string         `json:"gripper,omitempty"`
	GripperPositionKey string         `json:"gripper_position_key,omitempty"`
	SourceArm          string         `json:"source_arm,omitempty"`
	SourceGripper      string         `json:"source_gripper,omitempty"`
	DatasetID          string         `json:"dataset_id,omitempty"`
	ArmExtra           map[string]any `json:"arm_extra,omitempty"`
}

func (cfg *Config) Validate(path string) ([]string, []string, error) {
	if cfg.Arm == "" {
		return nil, nil, resource.NewConfigValidationFieldRequiredError(path, "arm")
	}
	if cfg.SourceGripper != "" && cfg.Gripper == "" {
		return nil, nil, fmt.Errorf("%s: source_gripper requires gripper", path)
	}
	deps := []string{cfg.Arm}
	if cfg.Gripper != "" {
		deps = append(deps, cfg.Gripper)
	}
	return deps, nil, nil
}

func (cfg *Config) sourceArm() string {
	if cfg.SourceArm != "" {
		return cfg.SourceArm
	}
	return cfg.Arm
}

func (cfg *Config) sourceGripper() string {
	if cfg.SourceGripper != "" {
		return cfg.SourceGripper
	}
	return cfg.Gripper
}

func (cfg *Config) gripperKey() string {
	if cfg.GripperPositionKey != "" {
		return cfg.GripperPositionKey
	}
	return "position_percentage"
}

// armExtra is passed to every paced MoveToJointPositions. The default is the SO-101
// driver's streaming-setpoint mode; drivers that don't know these keys ignore them.
func (cfg *Config) armExtra() map[string]any {
	if cfg.ArmExtra != nil {
		return cfg.ArmExtra
	}
	return map[string]any{"wait": false, "streamed": true}
}

type armPlayback struct {
	resource.AlwaysRebuild
	resource.Named
	logger logging.Logger
	cfg    *Config
	arm    arm.Arm
	player *player
	clock  clock

	// cloud access; tests replace these
	fetch func(ctx context.Context, id string) (*Sequence, error)
	list  func(ctx context.Context) ([]map[string]any, error)
	viam  *app.ViamClient

	mu        sync.Mutex
	cache     map[string]*Sequence
	sequence  string // active or most recent sequence id
	lastError string
	cancel    context.CancelFunc // non-nil while playing
	done      chan struct{}
}

func NewArmPlayback(ctx context.Context, deps resource.Dependencies, rawConf resource.Config, logger logging.Logger) (resource.Resource, error) {
	cfg, err := resource.NativeConfig[*Config](rawConf)
	if err != nil {
		return nil, err
	}
	a, err := arm.FromDependencies(deps, cfg.Arm)
	if err != nil {
		return nil, err
	}
	s := &armPlayback{
		Named:  rawConf.ResourceName().AsNamed(),
		logger: logger,
		cfg:    cfg,
		arm:    a,
		clock:  realClock{},
		cache:  map[string]*Sequence{},
		player: &player{arm: a, armExtra: cfg.armExtra(), gripperKey: cfg.gripperKey()},
	}
	if cfg.Gripper != "" {
		if s.player.gripper, err = gripper.FromDependencies(deps, cfg.Gripper); err != nil {
			return nil, err
		}
	}

	// The module manager supplies VIAM_API_KEY / VIAM_API_KEY_ID. Without them the
	// resource still builds; play and list report the problem when called.
	s.viam, err = app.CreateViamClientFromEnvVars(ctx, nil, logger)
	if err != nil {
		logger.Warnw("cloud client unavailable; play and list will fail", "error", err)
		s.fetch = func(context.Context, string) (*Sequence, error) {
			return nil, fmt.Errorf("cloud client unavailable: %w", err)
		}
		s.list = func(context.Context) ([]map[string]any, error) {
			return nil, fmt.Errorf("cloud client unavailable: %w", err)
		}
	} else {
		dc := s.viam.DataClient()
		s.fetch = func(ctx context.Context, id string) (*Sequence, error) {
			return fetchSequence(ctx, dc, id, cfg.sourceArm(), cfg.sourceGripper(), cfg.gripperKey())
		}
		s.list = func(ctx context.Context) ([]map[string]any, error) { return listSequences(ctx, dc, cfg.DatasetID) }
	}
	return s, nil
}

func (s *armPlayback) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	switch cmd["command"] {
	case "play":
		return s.play(ctx, cmd)
	case "stop":
		return s.stop()
	case "status":
		return s.status(), nil
	case "list":
		if s.cfg.DatasetID == "" {
			return nil, errors.New("list requires dataset_id in the service config")
		}
		seqs, err := s.list(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]any{"sequences": seqs}, nil
	}
	return nil, fmt.Errorf("unknown command %q; expected play, stop, status, or list", cmd["command"])
}

// toInputs converts captured degrees to the arm's inputs, the inverse of the capture
// collector. Without kinematics every joint is treated as revolute (degrees to radians).
func (s *armPlayback) toInputs(ctx context.Context, deg [][]float64) ([][]referenceframe.Input, error) {
	kin, err := s.arm.Kinematics(ctx)
	if err != nil {
		s.logger.Debugw("no kinematics; converting all joints degrees->radians", "error", err)
		kin = nil
	}
	out := make([][]referenceframe.Input, len(deg))
	for i, d := range deg {
		vals := make([]float64, len(d))
		copy(vals, d)
		if out[i], err = referenceframe.InputsFromJointPositions(kin, &pb.JointPositions{Values: vals}); err != nil {
			return nil, fmt.Errorf("frame %d: %w", i, err)
		}
	}
	return out, nil
}

func (s *armPlayback) play(ctx context.Context, cmd map[string]any) (map[string]any, error) {
	id, _ := cmd["sequence_id"].(string)
	if id == "" {
		return nil, errors.New("play requires a non-empty sequence_id")
	}
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("already playing %q; stop it first", s.sequence)
	}
	seq := s.cache[id]
	s.mu.Unlock()

	if seq == nil {
		var err error
		if seq, err = s.fetch(ctx, id); err != nil {
			return nil, err
		}
	}
	current, err := s.arm.JointPositions(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("reading arm joints: %w", err)
	}
	if len(current) != len(seq.JointsDeg[0]) {
		return nil, fmt.Errorf("joint count mismatch: arm has %d, sequence %q has %d", len(current), id, len(seq.JointsDeg[0]))
	}
	inputs, err := s.toInputs(ctx, seq.JointsDeg)
	if err != nil {
		return nil, err
	}
	useGripper := s.player.gripper != nil && seq.HasGripper()
	if s.player.gripper != nil && !seq.HasGripper() {
		s.logger.Infow("sequence has no gripper data; playing arm only", "sequence_id", id)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return nil, fmt.Errorf("already playing %q; stop it first", s.sequence)
	}
	s.cache[id] = seq
	s.sequence = id
	s.lastError = ""
	wctx, cancel := context.WithCancel(context.Background())
	s.cancel, s.done = cancel, make(chan struct{})
	s.player.clock = s.clock
	go s.worker(wctx, seq, inputs)
	s.logger.Infow("playback started", "sequence_id", id, "frames", len(inputs), "duration", seq.Duration())
	return map[string]any{
		"status": "playing", "sequence_id": id, "frame_count": len(inputs),
		"duration_s": seq.Duration().Seconds(), "has_gripper": useGripper,
	}, nil
}

func (s *armPlayback) worker(ctx context.Context, seq *Sequence, inputs [][]referenceframe.Input) {
	err := s.player.run(ctx, seq, inputs)
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case err == nil:
		s.logger.Infow("playback complete", "sequence_id", seq.ID)
	case ctx.Err() != nil:
		s.logger.Infow("playback stopped", "sequence_id", seq.ID)
	default:
		s.lastError = err.Error()
		s.logger.Errorw("playback failed", "sequence_id", seq.ID, "error", err)
	}
	s.cancel = nil
	close(s.done)
}

func (s *armPlayback) stop() (map[string]any, error) {
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	s.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
	return map[string]any{"status": "stopped"}, nil
}

func (s *armPlayback) status() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := map[string]any{"state": "idle", "sequence_id": s.sequence, "last_error": s.lastError,
		"frame_index": int(s.player.frameIndex.Load())}
	if seq := s.cache[s.sequence]; seq != nil {
		st["frame_count"] = len(seq.JointsDeg)
	}
	if s.cancel != nil {
		st["state"] = "playing"
	}
	return st
}

func (s *armPlayback) Close(context.Context) error {
	s.stop()
	if s.viam != nil {
		return s.viam.Close()
	}
	return nil
}
