package spec

// Level is an RFC 2119 / RFC 8174 requirement level.
type Level string

const (
	MUST        Level = "MUST"
	MUSTNOT     Level = "MUST NOT"
	SHOULD      Level = "SHOULD"
	SHOULDNOT   Level = "SHOULD NOT"
	MAY         Level = "MAY"
	RECOMMENDED Level = "RECOMMENDED"
	OPTIONAL    Level = "OPTIONAL"
)

// ValidLevels is the set of permitted RFC 2119 levels.
var ValidLevels = map[Level]bool{
	MUST:        true,
	MUSTNOT:     true,
	SHOULD:      true,
	SHOULDNOT:   true,
	MAY:         true,
	RECOMMENDED: true,
	OPTIONAL:    true,
}

// Status indicates whether a requirement is implemented, known gap, or non-goal.
type Status string

const (
	Covered Status = "covered"
	Gap     Status = "gap"
	NonGoal Status = "non-goal"
)

// ValidStatuses is the set of permitted conformance statuses.
var ValidStatuses = map[Status]bool{
	Covered: true,
	Gap:     true,
	NonGoal: true,
}

// Requirement represents a single normative clause in a specification.
type Requirement struct {
	Spec    string   `json:"spec"`
	Section string   `json:"section"`
	Level   Level    `json:"level"`
	Text    string   `json:"text"`
	Tests   []string `json:"tests"`
	Status  Status   `json:"status"`
	Note    string   `json:"note,omitempty"`
}

// Matrix pairs a named specification matrix with the relative package directory of tests covering it.
type Matrix struct {
	Name         string
	TestDir      string
	Requirements []Requirement
}

// Matrices returns all registered requirement matrices.
var Matrices = []Matrix{
	{
		Name:         "jmap-calendars",
		TestDir:      "jmap",
		Requirements: JMAPCalendarsRequirements,
	},
	{
		Name:         "jmap-mail",
		TestDir:      "jmap",
		Requirements: JMAPMailRequirements,
	},
	{
		Name:         "jscontact",
		TestDir:      "jmap/vcardconv",
		Requirements: JSContactRequirements,
	},
	{
		Name:         "smtp",
		TestDir:      "smtp",
		Requirements: SMTPRequirements,
	},
}
