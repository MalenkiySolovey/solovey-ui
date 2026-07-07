# Inbound Protection Draft

This directory intentionally has no `component.json`, so it is not registered or built as an installable component yet.

The current boundary is:

- `fallback-html` owns public decoy content, publish artifacts, site safety checks, target validation, and self-steal inbound draft handoff.
- `inbound-protection` will own the panel-wide protection policy for all protectable resources: panel listeners, proxy inbounds, public sites, node control endpoints, stream/SNI graylist, firewall/port posture, and reversible port ownership operations.

Design rule: no component should import this draft component. When it becomes real, it must discover resources through generic host registries and component hooks. Missing component means missing protection UI and missing protection workers, not dead `if/else` branches inside `fallback-html`, core, or other components.

Keep draft code behind explicit build tags until the component is ready to be registered.
