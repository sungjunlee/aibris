package adapter

import (
	"encoding/json"
	"path/filepath"
)

func (e *cwdMetadataExtractor) feed(data []byte) string {
	var foundCWD string
	for i := 0; i < len(data); {
		b := data[i]
		if !isJSONWhitespace(b) {
			e.recordContent = true
		}
		if e.invalid {
			i++
			continue
		}
		if e.inString {
			if cwd := e.feedStringByte(b); cwd != "" {
				if foundCWD == "" {
					foundCWD = cwd
				}
			}
			i++
			continue
		}
		if e.literalRemaining != "" {
			if b != e.literalRemaining[0] {
				e.invalid = true
			} else {
				e.literalRemaining = e.literalRemaining[1:]
				if e.literalRemaining == "" {
					e.finishValue()
				}
			}
			i++
			continue
		}
		if e.numberState != jsonNumberNone {
			if e.feedNumberByte(b) {
				i++
				continue
			}
			if !e.numberCanEnd() || !isJSONValueDelimiter(b) {
				e.invalid = true
				i++
				continue
			}
			e.numberState = jsonNumberNone
			e.finishValue()
			continue
		}
		if e.complete {
			if !isJSONWhitespace(b) {
				e.invalid = true
			}
			i++
			continue
		}
		if !e.started {
			if isJSONWhitespace(b) {
				i++
				continue
			}
			if b != '{' {
				e.invalid = true
				i++
				continue
			}
			e.started = true
			e.containers = append(e.containers, jsonContainer{
				kind:  '{',
				state: jsonObjectKeyOrEnd,
			})
			i++
			continue
		}
		if isJSONWhitespace(b) {
			i++
			continue
		}
		if len(e.containers) == 0 {
			e.invalid = true
			i++
			continue
		}

		frame := &e.containers[len(e.containers)-1]
		switch frame.state {
		case jsonObjectKeyOrEnd:
			if b == '}' {
				e.closeContainer('}')
			} else if b == '"' {
				e.startKeyString()
			} else {
				e.invalid = true
			}
		case jsonObjectKey:
			if b == '"' {
				e.startKeyString()
			} else {
				e.invalid = true
			}
		case jsonObjectColon:
			if b == ':' {
				frame.state = jsonObjectValue
			} else {
				e.invalid = true
			}
		case jsonObjectValue, jsonArrayValueOrEnd, jsonArrayValue:
			if frame.state == jsonArrayValueOrEnd && b == ']' {
				e.closeContainer(']')
			} else {
				e.startValue(b)
			}
		case jsonObjectCommaOrEnd:
			if b == ',' {
				frame.state = jsonObjectKey
			} else if b == '}' {
				e.closeContainer('}')
			} else {
				e.invalid = true
			}
		case jsonArrayCommaOrEnd:
			if b == ',' {
				frame.state = jsonArrayValue
			} else if b == ']' {
				e.closeContainer(']')
			} else {
				e.invalid = true
			}
		}
		i++
	}
	return foundCWD
}

func (e *cwdMetadataExtractor) startKeyString() {
	e.inString = true
	e.stringIsKey = true
	e.stringIsCWD = false
	e.escaped = false
	e.unicodeDigits = 0
	e.keyMatched = len(e.containers) == 1
	e.keyLength = 0
}

func (e *cwdMetadataExtractor) startValue(b byte) {
	switch b {
	case '"':
		frame := &e.containers[len(e.containers)-1]
		e.inString = true
		e.stringIsKey = false
		e.stringIsCWD = len(e.containers) == 1 && frame.kind == '{' && frame.keyIsCWD
		e.escaped = false
		e.unicodeDigits = 0
		e.cwdRaw = e.cwdRaw[:0]
		e.cwdTooLong = false
		if e.stringIsCWD {
			e.cwdRaw = append(e.cwdRaw, '"')
		}
	case '{':
		e.containers = append(e.containers, jsonContainer{
			kind:  '{',
			state: jsonObjectKeyOrEnd,
		})
	case '[':
		e.containers = append(e.containers, jsonContainer{
			kind:  '[',
			state: jsonArrayValueOrEnd,
		})
	case 't':
		e.literalRemaining = "rue"
	case 'f':
		e.literalRemaining = "alse"
	case 'n':
		e.literalRemaining = "ull"
	case '-':
		e.numberState = jsonNumberAfterMinus
	case '0':
		e.numberState = jsonNumberZero
	default:
		if b >= '1' && b <= '9' {
			e.numberState = jsonNumberInteger
		} else {
			e.invalid = true
		}
	}
}

func (e *cwdMetadataExtractor) feedStringByte(b byte) string {
	if e.stringIsCWD && !e.cwdTooLong {
		e.cwdRaw = append(e.cwdRaw, b)
		if len(e.cwdRaw) > maxRecordedCWDBytes {
			e.cwdRaw = nil
			e.cwdTooLong = true
		}
	}
	if e.unicodeDigits > 0 {
		if !isJSONHexDigit(b) {
			e.invalid = true
			return ""
		}
		e.unicodeDigits--
		return ""
	}
	if e.escaped {
		e.escaped = false
		if e.stringIsKey {
			e.keyMatched = false
		}
		switch b {
		case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
		case 'u':
			e.unicodeDigits = 4
		default:
			e.invalid = true
		}
		return ""
	}
	if b == '\\' {
		e.escaped = true
		return ""
	}
	if b < 0x20 {
		e.invalid = true
		return ""
	}
	if b != '"' {
		if e.stringIsKey {
			e.matchKeyByte(b)
		}
		return ""
	}

	e.inString = false
	if e.stringIsKey {
		frame := &e.containers[len(e.containers)-1]
		frame.keyIsCWD = e.keyMatched && e.keyLength == len("cwd")
		if frame.keyIsCWD {
			e.cwdFieldSeen = true
		}
		frame.state = jsonObjectColon
		return ""
	}
	if e.stringIsCWD && !e.cwdTooLong {
		var cwd string
		if json.Unmarshal(e.cwdRaw, &cwd) == nil && filepath.IsAbs(cwd) {
			e.finishValue()
			return filepath.Clean(cwd)
		}
	}
	e.finishValue()
	return ""
}

func (e *cwdMetadataExtractor) closeContainer(closing byte) {
	frame := e.containers[len(e.containers)-1]
	if (closing == '}' && frame.kind != '{') || (closing == ']' && frame.kind != '[') {
		e.invalid = true
		return
	}
	e.containers = e.containers[:len(e.containers)-1]
	if len(e.containers) == 0 {
		e.complete = true
		return
	}
	e.finishValue()
}

func (e *cwdMetadataExtractor) finishValue() {
	if len(e.containers) == 0 {
		e.complete = true
		return
	}
	frame := &e.containers[len(e.containers)-1]
	switch frame.state {
	case jsonObjectValue:
		frame.state = jsonObjectCommaOrEnd
		frame.keyIsCWD = false
	case jsonArrayValueOrEnd, jsonArrayValue:
		frame.state = jsonArrayCommaOrEnd
	default:
		e.invalid = true
	}
}

func (e *cwdMetadataExtractor) feedNumberByte(b byte) bool {
	switch e.numberState {
	case jsonNumberAfterMinus:
		if b == '0' {
			e.numberState = jsonNumberZero
			return true
		}
		if b >= '1' && b <= '9' {
			e.numberState = jsonNumberInteger
			return true
		}
	case jsonNumberZero:
		if b == '.' {
			e.numberState = jsonNumberAfterDot
			return true
		}
		if b == 'e' || b == 'E' {
			e.numberState = jsonNumberAfterExponent
			return true
		}
	case jsonNumberInteger:
		if b >= '0' && b <= '9' {
			return true
		}
		if b == '.' {
			e.numberState = jsonNumberAfterDot
			return true
		}
		if b == 'e' || b == 'E' {
			e.numberState = jsonNumberAfterExponent
			return true
		}
	case jsonNumberAfterDot:
		if b >= '0' && b <= '9' {
			e.numberState = jsonNumberFraction
			return true
		}
	case jsonNumberFraction:
		if b >= '0' && b <= '9' {
			return true
		}
		if b == 'e' || b == 'E' {
			e.numberState = jsonNumberAfterExponent
			return true
		}
	case jsonNumberAfterExponent:
		if b == '+' || b == '-' {
			e.numberState = jsonNumberAfterExponentSign
			return true
		}
		if b >= '0' && b <= '9' {
			e.numberState = jsonNumberExponent
			return true
		}
	case jsonNumberAfterExponentSign:
		if b >= '0' && b <= '9' {
			e.numberState = jsonNumberExponent
			return true
		}
	case jsonNumberExponent:
		if b >= '0' && b <= '9' {
			return true
		}
	}
	return false
}

func (e *cwdMetadataExtractor) numberCanEnd() bool {
	return e.numberState == jsonNumberZero ||
		e.numberState == jsonNumberInteger ||
		e.numberState == jsonNumberFraction ||
		e.numberState == jsonNumberExponent
}

func (e *cwdMetadataExtractor) matchKeyByte(b byte) {
	const key = "cwd"
	if e.keyLength >= len(key) || key[e.keyLength] != b {
		e.keyMatched = false
	}
	e.keyLength++
}

func isJSONValueDelimiter(b byte) bool {
	return isJSONWhitespace(b) || b == ',' || b == '}' || b == ']'
}

func isJSONHexDigit(b byte) bool {
	return (b >= '0' && b <= '9') ||
		(b >= 'a' && b <= 'f') ||
		(b >= 'A' && b <= 'F')
}

func isJSONWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n'
}
