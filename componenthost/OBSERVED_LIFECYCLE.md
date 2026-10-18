# Component lifecycle observation

The legacy `installed`, `enabled` and `active` fields retain their eligibility
meaning. In particular, `active` is not evidence that Start succeeded or that a
service is ready. Frontend loading continues to use installed/active eligibility.

`observedLifecycle` is additive in `/api/components` and the Panel Update
inventory. Only the supervisor publishes actual Start/Stop call results, after
the existing panic boundary returns. No feature error text is stored in this
projection. `observedAt` is the UTC call completion observation, not a probe time.
Repeated reconciliation without a call does not refresh it.

| Phase | instanceRetention | Meaning |
| --- | --- | --- |
| STARTED | RETAINED | Start returned successfully; supervisor retained the instance |
| START_FAILED | UNKNOWN | Start failed; partial feature effects are not disproved |
| STOPPED | RELEASED | Stop returned successfully; supervisor released its instance |
| STOP_FAILED | RETAINED | Stop failed; the same instance remains for retry |
| NOT_OBSERVED | UNKNOWN | No result in this supervisor incarnation |
| BINARY_ABSENT | UNKNOWN | Installed owner metadata exists, code is absent |
| NOT_INSTALLED | UNKNOWN | Owner is not installed and no call result exists |

Instance retention says nothing about external resource authority. For example,
Protection may preserve rules after a successful Stop. Removal still needs the
semantic owner's resource/authority checks.

The current-process map is bounded by the existing 256-owner metadata limit.
At capacity an unrecorded owner remains UNKNOWN. A new supervisor starts a new
epoch; late results from a prior publisher cannot overwrite it. No database,
worker, event history or generic Health framework is introduced.

Actual results take precedence over metadata-derived phases, so a failed Stop
cannot disappear merely because eligibility or installed metadata changed.
Installed owners absent from the binary remain visible with
`availableInBinary=false`, no invented manifest/version, and no UI action.
Older frontend/backend responses without the additive fields remain supported.
