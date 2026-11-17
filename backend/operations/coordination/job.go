package operationcoordination

import canonical "github.com/MalenkiySolovey/solovey-host/operationref"

type Progress = canonical.Progress
type Job = canonical.Job

func ProjectJob(change Change, progress *Progress, criticalSection bool) Job {
	return canonical.ProjectJob(change, progress, criticalSection)
}
