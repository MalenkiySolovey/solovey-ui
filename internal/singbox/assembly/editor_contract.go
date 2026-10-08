package assembly

import (
	entityinbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/inbounds"
	entityoutbounds "github.com/MalenkiySolovey/solovey-ui/internal/entities/outbounds"
	singboxconfig "github.com/MalenkiySolovey/solovey-ui/internal/singbox/config"
)

// EditorContract collects the existing domain owners' presentation facts at
// the same dependency-safe composition seam as complete candidate assembly.
func EditorContract() singboxconfig.CoreEditorContract {
	facts := singboxconfig.EditorContract()
	facts.CompatibilityCatalogue = append(facts.CompatibilityCatalogue, entityinbounds.CompatibilityCatalogue()...)
	facts.CompatibilityCatalogue = append(facts.CompatibilityCatalogue, entityoutbounds.CompatibilityCatalogue()...)
	return facts
}
