package publicsurface

import canonical "github.com/MalenkiySolovey/solovey-host/componenthost/publicsurface"

const maxClassifiedInputBytes = 4096

func ClassifyPath(raw string, reserved bool) string { return canonical.ClassifyPath(raw, reserved) }
func ClassifyUserAgent(raw string) string           { return canonical.ClassifyUserAgent(raw) }
func ClassifyMethod(raw string) string              { return canonical.ClassifyMethod(raw) }
func ClassifyStatus(status int) string              { return canonical.ClassifyStatus(status) }
func ClassifyBytes(value int64) string              { return canonical.ClassifyBytes(value) }
func ClassifyDuration(value int64) string           { return canonical.ClassifyDuration(value) }
