package schedule

import (
	"encoding/json"
	"fmt"
	"time"
)

// A Schedule holds 1) a pagerduty oncall schedule and 2) the data needed to
// generate/extend the oncall schedule.
type Schedule struct {
	// A list of users to schedule. The first user listed will be primary on the
	// first generated shift and the second user will be secondary. Upon schedule
	// generation, the users field will be updated to indicate who is primary
	// next.
	Users []string
	// The start date of the first rotation.
	Start time.Time
	// How long a single rotation lasts.
	// Formatted as a Go Duration (https://golang.org/pkg/time/#ParseDuration).
	RotationLength string
	rotationLength time.Duration
	// A duration -- how far out to schedule rotations.
	ScheduleFor string
	scheduleFor time.Duration

	// The oncall rotations. This is generated by the scheduler, but may be
	// modified by hand. Modifications will be reflected in the machine-friendly
	// Primary and Secondary rotations after the next schedule generation
	// invocation.
	Rotations []Rotation

	// Used to truncate Rotations in a test-friendly way.
	now time.Time
}

type Rotation struct {
	Start time.Time
	Primary string
	Secondary string
}

func (r Rotation) String() string {
	return fmt.Sprintf("%s %s %s", r.Start.Format(time.RFC3339), r.Primary, r.Secondary)
}

func NewSchedule(text []byte) (*Schedule, error) {
	s := &Schedule{}
	if err := json.Unmarshal(text, s); err != nil {
		return nil, fmt.Errorf("error parsing schedule: %s", err)
	}
	if d, err := time.ParseDuration(s.RotationLength); err != nil {
		return nil, fmt.Errorf("error parsing RotationLength: %s", err)
	} else {
		s.rotationLength = d
	}
	if d, err := time.ParseDuration(s.ScheduleFor); err != nil {
		return nil, fmt.Errorf("error parsing ScheduleFor: %s", err)
	} else {
		s.scheduleFor = d
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s Schedule) Validate() error {
	if len(s.Users) == 0 {
		return fmt.Errorf("must provide at least 1 user")
	}
	if s.rotationLength <= 0 {
		return fmt.Errorf("cannot have nonpositive RotationLength (got %s)", s.rotationLength)
	}
	return nil
}

func (s *Schedule) Generate() (*Schedule, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}

	ns := &Schedule{
		Users: s.Users,
		RotationLength: s.RotationLength,
		rotationLength: s.rotationLength,
		ScheduleFor: s.ScheduleFor,
		scheduleFor: s.scheduleFor,
		Rotations: s.Rotations[:],
		now: s.now,
	}

	if len(ns.Rotations) == 0 {
		// If we're generating a schedule from scratch, seed Rotations with an
		// initial rotation.
		ns.Start = s.Start
		ns.addRotation()
	} else {
		ns.Start = ns.Rotations[len(ns.Rotations)-1].Start.Add(ns.rotationLength)
	}

	if ns.now.IsZero() {
		ns.now = time.Now()
	}
	ns.Rotations = truncate(ns.Rotations, ns.now)

	for end := ns.now.Add(s.scheduleFor); end.After(ns.Rotations[len(ns.Rotations)-1].Start); {
		ns.addRotation()
	}

	return ns, nil
}

// Add a rotation to Rotations and update relevant state.
func (s *Schedule) addRotation() {
	r := Rotation{
		Start: s.Start,
		Primary: s.Users[0],
		Secondary: s.Users[1 % len(s.Users)],
	}
	s.Rotations = append(s.Rotations, r)
	s.Start = s.Start.Add(s.rotationLength)
	s.Users = append(s.Users[1:], s.Users[0])
}

// Truncate rotations that have elapsed.
func truncate(rs []Rotation, now time.Time) []Rotation {
	trunc := len(rs) - 1
	for ;trunc > 0; trunc -= 1 {
		if rs[trunc].Start.Before(now) {
			// Truncate all rotations _before_ this once, since it should be the
			// currently active rotation.
			trunc -= 1
			break
		}
	}
	return rs[trunc:]
}

func numRotations(start, end time.Time, duration time.Duration) int {
	length := end.Sub(start)
	if length <= 0 {
		return 0
	}

	rotations := int(length / duration)
	if length % duration > 0 {
		rotations += 1
	}
	return rotations
}
