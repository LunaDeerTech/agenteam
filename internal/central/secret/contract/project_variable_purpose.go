package contract

// ProjectVariable is dedicated Project storage ownership. It is deliberately
// excluded from legacy Purpose.Valid: old writes, references and lease consumers
// must not acquire this capability by passing a newly recognized string.
const ProjectVariable Purpose = "project_variable"
