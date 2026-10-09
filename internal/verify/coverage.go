package verify

import (
	"strings"

	"backupharbor/internal/i18n"
)

// buildCoverage states, in plain words, what this result does and does not prove.
// Only things that actually passed are claimed.
func (x *targetRun) buildCoverage() {
	tr, t := x.tr, x.t
	add := func(code string, args ...any) { tr.Coverage = append(tr.Coverage, i18n.M(code, args...)) }

	switch chk := tr.Stage(StageCheck); {
	case chk == nil || chk.Status == Skipped:
		add("cov.check_disabled")
	case chk.Status == Passed:
		switch t.Check.ReadData {
		case "all":
			add("cov.check_all")
		case "subset":
			add("cov.check_subset", t.Check.Subset)
		default:
			add("cov.check_structure")
		}
	default:
		add("cov.check_not_proven")
	}

	switch rs := tr.Stage(StageRestore); {
	case rs == nil || rs.Status == Skipped:
		add("cov.restore_disabled")
	case rs.Status == Passed:
		if len(t.Restore.Include) > 0 {
			add("cov.restore_filtered", strings.Join(t.Restore.Include, ", "))
		} else {
			add("cov.restore_full")
		}
		if t.Restore.VerifyOn() {
			add("cov.restore_verified")
		}
		if tr.Restore != nil && tr.Restore.MetadataWarnings > 0 {
			add("cov.metadata_warnings", tr.Restore.MetadataWarnings)
		}
	case rs.Status == Partial:
		add("cov.restore_incomplete")
	default:
		add("cov.restore_not_proven")
	}

	if cs := tr.Stage(StageContent); cs != nil && cs.Status != Skipped {
		if cs.Status == Passed || cs.Status == Partial {
			checked := 0
			for _, r := range cs.Rules {
				checked += r.Checked
			}
			add("cov.content", len(cs.Rules), checked)
		} else {
			add("cov.content_not_proven")
		}
	} else if len(t.Rules) == 0 && t.Restore.On() {
		add("cov.no_content_rules")
	}
	add("cov.limits")
}
