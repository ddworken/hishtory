package shared

import (
	"fmt"
	"regexp"
	"strconv"
)

type ParsedVersion struct {
	MajorVersion int
	MinorVersion int
}

func (pv ParsedVersion) GreaterThan(other ParsedVersion) bool {
	if pv.MajorVersion == other.MajorVersion && pv.MinorVersion == other.MinorVersion {
		return false
	}
	return !pv.LessThan(other)
}

func (pv ParsedVersion) LessThan(other ParsedVersion) bool {
	if pv.MajorVersion != other.MajorVersion {
		return pv.MajorVersion < other.MajorVersion
	}
	return pv.MinorVersion < other.MinorVersion
}

func (pv ParsedVersion) Decrement() ParsedVersion {
	if pv.MinorVersion > 1 {
		return ParsedVersion{pv.MajorVersion, pv.MinorVersion - 1}
	}
	panic("cannot decrement() when MinorVersion == 0")
}

func (pv ParsedVersion) String() string {
	return fmt.Sprintf("v%d.%d", pv.MajorVersion, pv.MinorVersion)
}

func ParseVersionString(versionString string) (ParsedVersion, error) {
	re := regexp.MustCompile(`v(\d+)[.](\d+)`)
	matches := re.FindAllStringSubmatch(versionString, -1)
	if len(matches) != 1 {
		return ParsedVersion{}, fmt.Errorf("failed to parse version=%#v (matches=%#v)", versionString, matches)
	}
	if len(matches[0]) != 3 {
		return ParsedVersion{}, fmt.Errorf("failed to parse version=%#v (matches[0]=%#v)", versionString, matches[0])
	}
	MajorVersion, err := strconv.Atoi(matches[0][1])
	if err != nil {
		return ParsedVersion{}, fmt.Errorf("failed to parse major version %#v", matches[0][1])
	}
	MinorVersion, err := strconv.Atoi(matches[0][2])
	if err != nil {
		return ParsedVersion{}, fmt.Errorf("failed to parse minor version %#v", matches[0][2])
	}
	return ParsedVersion{MajorVersion, MinorVersion}, nil
}
