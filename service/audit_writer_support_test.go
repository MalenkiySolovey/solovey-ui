package service

import canonical "github.com/MalenkiySolovey/solovey-backend/audit/store"

func AuditDroppedTotal() uint64 { return canonical.DroppedTotal() }
