package service

func AuditDroppedTotal() uint64 { return auditDroppedTotal.Load() }
