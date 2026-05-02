package hermes

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Cron is a 5-field cron expression: minute hour day-of-month month day-of-week.
// Each field is one of: "*", a number, "*/N", "A,B,C", "A-B".
//
// We deliberately do NOT depend on robfig/cron — adding a third-party dep for
// what amounts to ~100 lines of Go is unjustified, and we get to inline the
// scheduling decision into the worker.
type Cron struct {
	mins   sched
	hours  sched
	doms   sched
	months sched
	dows   sched
	raw    string
}

type sched struct{ allowed map[int]bool }

// ParseCron returns a Cron or an error.
func ParseCron(expr string) (*Cron, error) {
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return nil, fmt.Errorf("cron: want 5 fields (m h dom mon dow), got %d", len(parts))
	}
	c := &Cron{raw: expr}
	var err error
	if c.mins, err = parseField(parts[0], 0, 59); err != nil {
		return nil, fmt.Errorf("minute: %w", err)
	}
	if c.hours, err = parseField(parts[1], 0, 23); err != nil {
		return nil, fmt.Errorf("hour: %w", err)
	}
	if c.doms, err = parseField(parts[2], 1, 31); err != nil {
		return nil, fmt.Errorf("dom: %w", err)
	}
	if c.months, err = parseField(parts[3], 1, 12); err != nil {
		return nil, fmt.Errorf("month: %w", err)
	}
	if c.dows, err = parseField(parts[4], 0, 6); err != nil {
		return nil, fmt.Errorf("dow: %w", err)
	}
	return c, nil
}

// String prints the original expression.
func (c *Cron) String() string { return c.raw }

// Next returns the next time on or after `from` that matches.
// Stepping minute-by-minute is fine — a cron tick is once a minute anyway.
func (c *Cron) Next(from time.Time) time.Time {
	t := from.Truncate(time.Minute).Add(time.Minute)
	for i := 0; i < 366*24*60; i++ { // 1-year safety bound
		if c.matches(t) {
			return t
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}
}

// Due returns true if the schedule's next fire is at or before `now`, given
// the schedule was last fired at `last` (zero value = never).
func (c *Cron) Due(now, last time.Time) bool {
	from := last
	if from.IsZero() {
		// First-fire policy: be conservative and fire at the next match.
		// Without this, a `*/30 * * * *` schedule added between :00 and :30
		// wouldn't fire until :30, missing one window. Acceptable.
		from = now.Add(-time.Minute)
	}
	next := c.Next(from)
	return !next.IsZero() && !next.After(now)
}

func (c *Cron) matches(t time.Time) bool {
	return c.mins.has(t.Minute()) &&
		c.hours.has(t.Hour()) &&
		c.doms.has(t.Day()) &&
		c.months.has(int(t.Month())) &&
		c.dows.has(int(t.Weekday()))
}

func (s sched) has(v int) bool {
	if s.allowed == nil {
		return true
	}
	return s.allowed[v]
}

func parseField(field string, lo, hi int) (sched, error) {
	if field == "*" {
		return sched{}, nil
	}
	out := map[int]bool{}
	for _, part := range strings.Split(field, ",") {
		step := 1
		if i := strings.Index(part, "/"); i >= 0 {
			n, err := strconv.Atoi(part[i+1:])
			if err != nil || n <= 0 {
				return sched{}, fmt.Errorf("bad step %q", part)
			}
			step = n
			part = part[:i]
		}
		from, to := lo, hi
		if part != "*" {
			if i := strings.Index(part, "-"); i >= 0 {
				a, err := strconv.Atoi(part[:i])
				if err != nil {
					return sched{}, fmt.Errorf("bad range %q", part)
				}
				b, err := strconv.Atoi(part[i+1:])
				if err != nil {
					return sched{}, fmt.Errorf("bad range %q", part)
				}
				from, to = a, b
			} else {
				n, err := strconv.Atoi(part)
				if err != nil {
					return sched{}, fmt.Errorf("bad value %q", part)
				}
				from, to = n, n
			}
		}
		for v := from; v <= to; v += step {
			if v < lo || v > hi {
				return sched{}, fmt.Errorf("value %d out of range [%d,%d]", v, lo, hi)
			}
			out[v] = true
		}
	}
	return sched{allowed: out}, nil
}
