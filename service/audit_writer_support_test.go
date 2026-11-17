package service

import canonical "github.com/MalenkiySolovey/solovey-ui/backend/audit/store"

func AuditDroppedTotal() uint64 { return canonical.DroppedTotal() }
