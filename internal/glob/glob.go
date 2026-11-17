package glob

func Match(pattern string, text string) bool {
	patternIndex, textIndex := 0, 0
	lastStarPatternIndex, textIndexAfterLastStar := -1, 0

	for textIndex < len(text) {
		if patternIndex < len(pattern) && pattern[patternIndex] == '*' {
			lastStarPatternIndex = patternIndex
			textIndexAfterLastStar = textIndex
			patternIndex++
			continue
		}

		if patternIndex < len(pattern) {
			matched, tokenLength := matchToken(pattern[patternIndex:], text[textIndex])
			if matched {
				patternIndex += tokenLength
				textIndex++
				continue
			}
		}

		if lastStarPatternIndex >= 0 {
			textIndexAfterLastStar++
			patternIndex = lastStarPatternIndex + 1
			textIndex = textIndexAfterLastStar
			continue
		}
		return false
	}

	for patternIndex < len(pattern) && pattern[patternIndex] == '*' {
		patternIndex++
	}
	return patternIndex == len(pattern)
}

func matchToken(pattern string, character byte) (matched bool, tokenLength int) {
	switch pattern[0] {
	case '?':
		return true, 1
	case '[':
		return matchCharacterClass(pattern, character)
	case '\\':
		if len(pattern) >= 2 {
			return pattern[1] == character, 2
		}
		return character == '\\', 1
	default:
		return pattern[0] == character, 1
	}
}

func matchCharacterClass(pattern string, character byte) (matched bool, tokenLength int) {
	index := 1
	negated := index < len(pattern) && pattern[index] == '^'
	if negated {
		index++
	}

	for index < len(pattern) && pattern[index] != ']' {
		switch {
		case pattern[index] == '\\' && index+1 < len(pattern):
			if pattern[index+1] == character {
				matched = true
			}
			index += 2
		case index+2 < len(pattern) && pattern[index+1] == '-':
			rangeStart, rangeEnd := pattern[index], pattern[index+2]
			if rangeStart > rangeEnd {
				rangeStart, rangeEnd = rangeEnd, rangeStart
			}
			if character >= rangeStart && character <= rangeEnd {
				matched = true
			}
			index += 3
		default:
			if pattern[index] == character {
				matched = true
			}
			index++
		}
	}

	hasClosingBracket := index < len(pattern)
	if hasClosingBracket {
		index++
	}
	if negated {
		matched = !matched
	}
	return matched, index
}
