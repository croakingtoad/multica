package handler

import (
	"encoding/json"

	"github.com/multica-ai/multica/server/internal/runtimehooks"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// hookResolutionInputs adapts stored snapshot rows to the resolution layer's
// inputs. The provider rules stay in server/internal/runtimehooks; this file
// only moves rows across the seam, so the projection can be tested without a
// database. Both the projection and the per-event answer come through here, so
// the expected-source set and the row conversion cannot diverge between them.
func hookResolutionInputs(provider string, rows []db.HookStateSnapshot) ([]runtimehooks.SourceRef, []runtimehooks.ObservedSource, error) {
	expected, err := expectedHookSources(provider)
	if err != nil {
		return nil, nil, err
	}
	refs := make([]runtimehooks.SourceRef, 0, len(expected))
	for _, key := range expected {
		refs = append(refs, runtimehooks.SourceRef{Scope: key.Scope, Format: key.Format})
	}
	observed := make([]runtimehooks.ObservedSource, 0, len(rows))
	for _, row := range rows {
		observed = append(observed, runtimehooks.ObservedSource{
			Source:        runtimehooks.SourceRef{Scope: row.Scope, Format: row.Format},
			SourcePath:    nullableString(row.SourcePath),
			ContentHash:   nullableString(row.ContentHash),
			Hooks:         json.RawMessage(row.Hooks),
			DisabledHooks: json.RawMessage(row.DisabledHooks),
		})
	}
	return refs, observed, nil
}

func hookResolution(provider string, rows []db.HookStateSnapshot) runtimehooks.Projection {
	refs, observed, err := hookResolutionInputs(provider, rows)
	if err != nil {
		return runtimehooks.Projection{
			Provider: provider,
			Entries:  []runtimehooks.ProjectedEntry{},
			Error:    err.Error(),
		}
	}
	return runtimehooks.Project(runtimehooks.Provider(provider), refs, observed)
}
