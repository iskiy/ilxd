// Copyright (c) 2022 The illium developers
// Use of this source code is governed by an MIT
// license that can be found in the LICENSE file.

package macros

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
)

var ErrCircularImports = errors.New("circular imports")

const LurkFileExtension = ".lurk"

type MacroPreprocessor struct {
	depDir         *fsDirectory
	removeComments bool
}

func NewMacroPreprocessor(opts ...Option) (*MacroPreprocessor, error) {
	var cfg config
	for _, opt := range opts {
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}

	return &MacroPreprocessor{
		depDir:         cfg.depDir,
		removeComments: cfg.removeComments,
	}, nil
}

func (p *MacroPreprocessor) Preprocess(lurkProgram string) (string, error) {
	if strings.Contains(lurkProgram, fmt.Sprintf("!(%s", Import.String())) {
		if p.depDir == nil {
			return "", errors.New("dependency directory not set")
		}

		// Recursively expand import macros and check for circular imports
		var err error
		lurkProgram, err = macroExpandImport(lurkProgram, p.depDir, nil)
		if err != nil {
			return "", err
		}
	}
	ret, err := preProcess(lurkProgram)
	if err != nil {
		return "", err
	}
	if p.removeComments {
		ret = removeComments(ret)
	}
	if !IsValidLurk(ret) {
		return "", errors.New("error preprocessing: mismatch parenthesis")
	}
	return ret, nil
}

var paramMap = map[string]string{
	"sighash":            "(car public-params)",
	"txo-root":           "(car (cdr (cdr public-params)))",
	"fee":                "(car (cdr (cdr (cdr public-params))))",
	"coinbase":           "(car (cdr (cdr (cdr (cdr public-params)))))",
	"mint-id":            "(car (cdr (cdr (cdr (cdr (cdr public-params))))))",
	"mint-amount":        "(car (cdr (cdr (cdr (cdr (cdr (cdr public-params)))))))",
	"locktime":           "(car (cdr (cdr (cdr (cdr (cdr (cdr (cdr (cdr public-params)))))))))",
	"locktime-precision": "(car (cdr (cdr (cdr (cdr (cdr (cdr (cdr (cdr (cdr public-params))))))))))",
}

var inputMap = map[string]string{
	"amount":           "(car %s)",
	"asset-id":         "(car (cdr %s))",
	"salt":             "(car (cdr (cdr %s)))",
	"state":            "(car (cdr (cdr (cdr %s))))",
	"commitment-index": "(car (cdr (cdr (cdr (cdr %s)))))",
	"inclusion-proof":  "(car (cdr (cdr (cdr (cdr (cdr %s))))))",
	"script":           "(car (cdr (cdr (cdr (cdr (cdr (cdr %s)))))))",
	"locking-params":   "(car (cdr (cdr (cdr (cdr (cdr (cdr (cdr %s))))))))",
	"unlocking-params": "(car (cdr (cdr (cdr (cdr (cdr (cdr (cdr (cdr %s)))))))))",
}

var outputMap = map[string]string{
	"script-hash": "(car %s)",
	"amount":      "(car (cdr %s))",
	"asset-id":    "(car (cdr (cdr %s)))",
	"salt":        "(car (cdr (cdr (cdr %s))))",
	"state":       "(car (cdr (cdr (cdr (cdr %s)))))",
}

var pubOutMap = map[string]string{
	"commitment": "(car %s)",
	"ciphertext": "(car (cdr %s))",
}

func loadFilesFromFS(fileSystem fs.FS, directory string) ([]string, error) {
	dirEntries, err := fs.ReadDir(fileSystem, directory)
	if err != nil {
		return nil, err
	}

	var fileContents []string
	for _, entry := range dirEntries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == LurkFileExtension {
			content, err := fs.ReadFile(fileSystem, filepath.Join(directory, entry.Name()))
			if err != nil {
				return nil, err
			}
			fileContents = append(fileContents, string(content))
		}
	}
	return fileContents, nil
}

func extractModule(files []string, moduleName string) (string, error) {
	moduleCount := 0
	moduleContent := ""

	for _, content := range files {
		p := NewParser(content)
		for p.Peek() != 0 {
			if strings.HasPrefix(p.input[p.pos:], "!(module") {
				p.pos += 9 // Skip over "!(module"
				nameStart := p.pos

				for p.Peek() != ' ' && p.Peek() != 0 {
					p.Consume()
				}

				name := p.input[nameStart:p.pos]
				if name == moduleName {
					moduleCount++

					for p.Peek() != '(' && p.Peek() != 0 {
						p.Consume()
					}
					if p.Peek() == '(' {
						p.Consume() // Skip over opening parenthesis
					}
					depth := 1
					moduleStart := p.pos
					for depth > 0 && p.Peek() != 0 {
						if p.Peek() == '(' {
							depth++
						} else if p.Peek() == ')' {
							depth--
						}
						if depth > 0 {
							p.Consume()
						}
					}
					moduleContent += p.input[moduleStart:p.pos-1] + "\n" // Exclude the closing parenthesis
				}
			} else {
				p.Consume()
			}
		}
	}

	if moduleCount > 1 {
		return "", fmt.Errorf("found multiple modules named %s", moduleName)
	} else if moduleCount == 0 {
		return "", fmt.Errorf("module %s not found", moduleName)
	}

	return moduleContent, nil
}

func extractModuleExpression(moduleContent, exprName string) (string, error) {
	expression := ""

	p := NewParser(moduleContent)
	for p.Peek() != 0 {
		if strings.HasPrefix(p.input[p.pos:], "!(defun") {
			startPos := p.pos
			p.pos += 8 // Skip over "!(defun"
			nameStart := p.pos

			for p.Peek() != ' ' && p.Peek() != 0 {
				p.Consume()
			}

			name := p.input[nameStart:p.pos]
			if name == exprName {
				depth := 1
				for depth > 0 && p.Peek() != 0 {
					if p.Peek() == '(' {
						depth++
					} else if p.Peek() == ')' {
						depth--
					}
					if depth > 0 {
						p.Consume()
					}
				}
				p.Consume()
				p.Consume()
				expression += p.input[startPos:p.pos-1] + "\n" // Exclude the closing parenthesis
			}
		} else if strings.HasPrefix(p.input[p.pos:], "!(def") {
			startPos := p.pos
			p.pos += 6 // Skip over "!(def"
			nameStart := p.pos

			for p.Peek() != ' ' && p.Peek() != 0 {
				p.Consume()
			}

			name := p.input[nameStart:p.pos]
			if name == exprName {
				depth := 1
				for depth > 0 && p.Peek() != 0 {
					if p.Peek() == '(' {
						depth++
					} else if p.Peek() == ')' {
						depth--
					}
					if depth > 0 {
						p.Consume()
					}
				}
				p.Consume()
				p.Consume()
				expression += p.input[startPos:p.pos-1] + "\n" // Exclude the closing parenthesis
			}
		} else if strings.HasPrefix(p.input[p.pos:], "!(defrec") {
			startPos := p.pos
			p.pos += 9 // Skip over "!(defrec"
			nameStart := p.pos

			for p.Peek() != ' ' && p.Peek() != 0 {
				p.Consume()
			}

			name := p.input[nameStart:p.pos]
			if name == exprName {
				depth := 1
				for depth > 0 && p.Peek() != 0 {
					if p.Peek() == '(' {
						depth++
					} else if p.Peek() == ')' {
						depth--
					}
					if depth > 0 {
						p.Consume()
					}
				}
				p.Consume()
				p.Consume()
				expression += p.input[startPos:p.pos-1] + "\n" // Exclude the closing parenthesis
			}
		} else {
			p.Consume()
		}
	}

	return expression, nil
}

func macroExpandImport(lurkProgram string, dependencyDir *fsDirectory, dependencyChain []string) (string, error) {
	var result string
	p := NewParser(lurkProgram)

	for p.Peek() != 0 {
		if strings.HasPrefix(p.input[p.pos:], "!(import") {
			p.pos += 9 // Skip over "!(import"
			importPathStart := p.pos

			for p.Peek() != ')' && p.Peek() != 0 {
				p.Consume()
			}

			pathAndModule := p.input[importPathStart:p.pos]

			depChainCpy := make([]string, len(dependencyChain))
			copy(depChainCpy, dependencyChain)

			for _, mod := range depChainCpy {
				if mod == pathAndModule {
					return "", fmt.Errorf("%w: %s", ErrCircularImports, strings.Join(depChainCpy, " -> "))
				}
			}
			depChainCpy = append(depChainCpy, pathAndModule)

			splits := strings.Split(pathAndModule, "/")

			if len(splits) < 1 {
				return "", fmt.Errorf("invalid import format")
			}

			// The last split is the module name, everything else is part of the directory.
			var moduleContent string
			secondPass := false
			for {
				moduleName := splits[len(splits)-1]
				exprName := ""
				dir := filepath.Join(append([]string{dependencyDir.path}, splits[:len(splits)-1]...)...)
				if secondPass {
					if len(splits) < 2 {
						return "", errors.New("dependency file not found")
					}
					moduleName = splits[len(splits)-2]
					exprName = splits[len(splits)-1]
					dir = filepath.Join(append([]string{dependencyDir.path}, splits[:len(splits)-2]...)...)
				}

				// If there was only the module name without any directory, use dependencyDirectoryPath as the directory.
				if (!secondPass && len(splits) == 1) || (secondPass && len(splits) == 2) {
					dir = dependencyDir.path
				}

				// Load files
				files, err := loadFilesFromFS(dependencyDir.fileSystem, dir)
				if err != nil {
					if secondPass {
						return "", err
					} else {
						secondPass = true
						continue
					}
				}
				// Extract module content
				moduleContent, err = extractModule(files, moduleName)
				if err != nil {
					return "", err
				}

				if secondPass {
					moduleContent, err = extractModuleExpression(moduleContent, exprName)
					if err != nil {
						return "", err
					}
				}

				break
			}

			// Before returning the expanded content, process imports within the moduleContent
			expandedModuleContent, err := macroExpandImport(moduleContent, dependencyDir, depChainCpy)
			if err != nil {
				return "", err
			}

			p.ReadUntil(')')
			p.Consume() // Consume the closing parenthesis after the import body

			result += expandedModuleContent
		} else {
			result += string(p.Consume())
		}
	}
	return result, nil
}

func macroExpandParam(lurkProgram string) string {
	p := NewParser(lurkProgram)
	result := ""

	for p.Peek() != 0 {
		if strings.HasPrefix(p.input[p.pos:], "!(param") {
			p.pos += 8 // Skip over "!(param"
			paramStart := p.pos

			for p.Peek() != ' ' && p.Peek() != ')' && p.Peek() != 0 {
				p.Consume()
			}
			paramName := p.input[paramStart:p.pos]

			if paramName == "nullifiers" {
				// Skip over potential whitespace
				for p.Peek() == ' ' {
					p.Consume()
				}
				indexStart := p.pos
				for p.Peek() != ')' && p.Peek() != 0 {
					p.Consume()
				}
				index := p.input[indexStart:p.pos]
				idx, err := strconv.Atoi(index)
				if err != nil {
					return ""
				}
				expr := "(car "
				for i := 0; i < idx; i++ {
					expr += "(cdr "
				}
				result += fmt.Sprintf("%s(car (cdr public-params))", expr)
				for i := 0; i < idx+1; i++ {
					result += ")"
				}
			} else if paramName == "priv-in" {
				// Skip over potential whitespace
				for p.Peek() == ' ' {
					p.Consume()
				}
				indexStart := p.pos
				for p.Peek() != ' ' && p.Peek() != ')' && p.Peek() != 0 {
					p.Consume()
				}
				index := p.input[indexStart:p.pos]
				idx, err := strconv.Atoi(index)
				if err != nil {
					return ""
				}
				expr := "(car "
				for i := 0; i < idx; i++ {
					expr += "(cdr "
				}
				resultExp := fmt.Sprintf("%s(car private-params)", expr)
				for i := 0; i < idx+1; i++ {
					resultExp += ")"
				}

				if p.Peek() == ' ' {
					// Consume whitespace and then check for sub-param
					p.Consume()
					subParamStart := p.pos
					for p.Peek() != ' ' && p.Peek() != ')' && p.Peek() != 0 {
						p.Consume()
					}
					subParam := p.input[subParamStart:p.pos]
					if subExpr, ok := inputMap[subParam]; ok {
						result += fmt.Sprintf(subExpr, resultExp)
					} else {
						result += resultExp
					}
				} else {
					result += resultExp
				}

			} else if paramName == "priv-out" {
				// Skip over potential whitespace
				for p.Peek() == ' ' {
					p.Consume()
				}
				indexStart := p.pos
				for p.Peek() != ' ' && p.Peek() != ')' && p.Peek() != 0 {
					p.Consume()
				}
				index := p.input[indexStart:p.pos]
				idx, err := strconv.Atoi(index)
				if err != nil {
					return ""
				}
				expr := "(car "
				for i := 0; i < idx; i++ {
					expr += "(cdr "
				}
				resultExp := fmt.Sprintf("%s(car (cdr private-params))", expr)
				//resultExp := fmt.Sprintf("%s(car (cdr (cdr (cdr (cdr (cdr (cdr (cdr public-params))))))))", expr)
				for i := 0; i < idx+1; i++ {
					resultExp += ")"
				}

				if p.Peek() == ' ' {
					// Consume whitespace and then check for sub-param
					p.Consume()
					subParamStart := p.pos
					for p.Peek() != ' ' && p.Peek() != ')' && p.Peek() != 0 {
						p.Consume()
					}
					subParam := p.input[subParamStart:p.pos]
					if subExpr, ok := outputMap[subParam]; ok {
						result += fmt.Sprintf(subExpr, resultExp)
					} else {
						result += resultExp
					}
				} else {
					result += resultExp
				}
			} else if paramName == "pub-out" {
				// Skip over potential whitespace
				for p.Peek() == ' ' {
					p.Consume()
				}
				indexStart := p.pos
				for p.Peek() != ' ' && p.Peek() != ')' && p.Peek() != 0 {
					p.Consume()
				}
				index := p.input[indexStart:p.pos]
				idx, err := strconv.Atoi(index)
				if err != nil {
					return ""
				}
				expr := "(car "
				for i := 0; i < idx; i++ {
					expr += "(cdr "
				}
				//resultExp := fmt.Sprintf("%s(car (cdr private-params))", expr)
				resultExp := fmt.Sprintf("%s(car (cdr (cdr (cdr (cdr (cdr (cdr (cdr public-params))))))))", expr)
				for i := 0; i < idx+1; i++ {
					resultExp += ")"
				}

				if p.Peek() == ' ' {
					// Consume whitespace and then check for sub-param
					p.Consume()
					subParamStart := p.pos
					for p.Peek() != ' ' && p.Peek() != ')' && p.Peek() != 0 {
						p.Consume()
					}
					subParam := p.input[subParamStart:p.pos]
					if subExpr, ok := pubOutMap[subParam]; ok {
						result += fmt.Sprintf(subExpr, resultExp)
					} else {
						result += resultExp
					}
				} else {
					result += resultExp
				}
			} else if substitution, found := paramMap[paramName]; found {
				result += substitution
			} else {
				// In case the paramName is not found, let's just keep the original code
				result += "!(param" + paramName + ")"
			}

			p.ReadUntil(')')
			p.Consume() // Consume the closing parenthesis after the param body
		} else {
			result += string(p.Consume())
		}
	}
	return result
}

func macroExpandList(lurkProgram string) string {
	for strings.Contains(lurkProgram, "!(list") {
		p := NewParser(lurkProgram)
		result := ""

		for p.Peek() != 0 {
			if strings.HasPrefix(p.input[p.pos:], "!(list") {
				p.pos += 7 // Skip over "!(list"
				var elements []string

				// Ensure we capture all elements and that we don't accidentally consume the closing parenthesis of !(list ... )
				for p.Peek() != ')' && p.Peek() != 0 {
					// Skip over potential whitespace
					for p.Peek() == ' ' {
						p.Consume()
					}
					var body string
					if p.Peek() == '(' {
						body = p.ParseSExpr() // Parse the s-expression if body starts with (
					} else {
						bodyStart := p.pos
						for p.Peek() != ' ' && p.Peek() != ')' && p.Peek() != 0 {
							p.Consume()
						}
						body = p.input[bodyStart:p.pos]
					}

					elements = append(elements, body)
				}

				p.ReadUntil(')')
				p.Consume() // Consume the closing parenthesis after the list body

				if len(elements) > 0 {
					result += buildConsList(elements)
				} else {
					result += "nil"
				}
			} else {
				result += string(p.Consume())
			}
		}
		lurkProgram = result
	}
	return lurkProgram
}

// Recursively builds a cons list from the elements
func buildConsList(elems []string) string {
	if len(elems) == 0 {
		return "nil"
	}
	if len(elems) == 1 {
		return fmt.Sprintf("(cons %s nil)", elems[0])
	}

	return fmt.Sprintf("(cons %s %s)", elems[0], buildConsList(elems[1:]))
}

func macroExpandAssert(lurkProgram string) string {
	p := NewParser(lurkProgram)
	result := ""

	for p.Peek() != 0 {
		if strings.HasPrefix(p.input[p.pos:], "!(assert") &&
			!strings.HasPrefix(p.input[p.pos:], "!(assert-eq") {
			p.pos += 9 // Skip over "!(assert"
			var body string
			if p.Peek() == '(' {
				body = p.ParseSExpr() // Parse the s-expression if body starts with (
			} else {
				bodyStart := p.pos
				for p.Peek() != ')' && p.Peek() != 0 {
					p.Consume()
				}
				body = p.input[bodyStart:p.pos]
			}
			result += fmt.Sprintf("(if (eq %s nil) nil", body)
			p.ReadUntil(')')
			p.Consume() // Consume the closing parenthesis after the assert body
		} else {
			result += string(p.Consume())
		}
	}
	return result
}

func macroExpandAssertEq(lurkProgram string) string {
	p := NewParser(lurkProgram)
	result := ""

	for p.Peek() != 0 {
		if strings.HasPrefix(p.input[p.pos:], "!(assert-eq") {
			p.pos += 12 // Skip over "!(assert-eq"

			var val1 string
			if p.Peek() == '(' {
				val1 = p.ParseSExpr() // Parse the s-expression if body starts with (
			} else {
				bodyStart := p.pos
				for p.Peek() != ')' && p.Peek() != 0 {
					p.Consume()
				}
				val1 = p.input[bodyStart:p.pos]
			}

			// Skip over potential whitespace
			for p.Peek() == ' ' {
				p.Consume()
			}

			var val2 string
			if p.Peek() == '(' {
				val2 = p.ParseSExpr() // Parse the s-expression if body starts with (
			} else {
				bodyStart := p.pos
				for p.Peek() != ')' && p.Peek() != 0 {
					p.Consume()
				}
				val2 = p.input[bodyStart:p.pos]
			}

			result += fmt.Sprintf("(if (eq (eq %s %s) nil) nil", val1, val2)
			p.ReadUntil(')')
			p.Consume() // Consume the closing parenthesis after the assert-eq body
		} else {
			result += string(p.Consume())
		}
	}
	return result
}

func macroExpandDef(lurkProgram string) string {
	for strings.Contains(lurkProgram, "!(def ") {
		p := NewParser(lurkProgram)
		result := ""

		for p.Peek() != 0 {
			if strings.HasPrefix(p.input[p.pos:], "!(def") &&
				!strings.HasPrefix(p.input[p.pos:], "!(defrec") &&
				!strings.HasPrefix(p.input[p.pos:], "!(defun") {
				p.pos += 6 // Skip over "!(def"
				variableName := strings.TrimSpace(p.ReadUntil(' '))
				p.Consume()
				var body string
				if p.Peek() == '(' {
					body = p.ParseSExpr() // Parse the s-expression if body starts with (
				} else {
					bodyStart := p.pos
					for p.Peek() != ')' && p.Peek() != 0 {
						p.Consume()
					}
					body = p.input[bodyStart:p.pos]
				}
				result += fmt.Sprintf("(let ((%s %s))", variableName, body)
				p.ReadUntil(')')
				p.Consume() // Consume the closing parenthesis after the def body
			} else {
				result += string(p.Consume())
			}
		}
		lurkProgram = result
	}
	return lurkProgram
}

func macroExpandDefrec(lurkProgram string) string {
	for strings.Contains(lurkProgram, "!(defrec") {
		p := NewParser(lurkProgram)
		result := ""

		for p.Peek() != 0 {
			if strings.HasPrefix(p.input[p.pos:], "!(defrec") {
				p.pos += 9 // Skip over "!(defrec"
				variableName := strings.TrimSpace(p.ReadUntil(' '))
				p.Consume()
				var body string
				if p.Peek() == '(' {
					body = p.ParseSExpr() // Parse the s-expression if body starts with (
				} else {
					bodyStart := p.pos
					for p.Peek() != ')' && p.Peek() != 0 {
						p.Consume()
					}
					body = p.input[bodyStart:p.pos]
				}
				result += fmt.Sprintf("(letrec ((%s %s))", variableName, body)
				p.ReadUntil(')')
				p.Consume() // Consume the closing parenthesis after the defrec body
			} else {
				result += string(p.Consume())
			}
		}
		lurkProgram = result
	}
	return lurkProgram
}

func macroExpandDefun(lurkProgram string) string {
	for strings.Contains(lurkProgram, "!(defun") {
		p := NewParser(lurkProgram)
		result := ""
		for p.Peek() != 0 {
			if strings.HasPrefix(p.input[p.pos:], "!(defun") {
				p.pos += 8 // Skip over "!(defun"
				name := strings.TrimSpace(p.ReadUntil('('))
				params := p.ParseSExpr()

				p.Consume()
				body := p.ParseSExpr()
				if len(body) >= 2 {
					b := removeComments(body)
					b = strings.ReplaceAll(b, " ", "")
					b = strings.ReplaceAll(b, "\n", "")
					b = strings.ReplaceAll(b, "\t", "")
					b = removeComments(b)
					if b[1] == '!' || b[1] == '(' {
						body = strings.TrimPrefix(body, "(")
						body = strings.TrimSuffix(body, ")")
					}
				}

				result += fmt.Sprintf("(letrec ((%s (lambda %s %s)))", name, params, body)
				p.ReadUntil(')')
				p.Consume() // Consume the closing parenthesis after the defun body
			} else {
				result += string(p.Consume())
			}
		}
		lurkProgram = result
	}
	return lurkProgram
}

// preProcess takes a lurk program string and expands all the macros
func preProcess(lurkProgram string) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(lurkProgram))

	var (
		openCount      = 0
		parenthesesMap = make(map[int]int)
		modifiedLines  []string
	)

	for scanner.Scan() {
		line := scanner.Text()
		var modifiedLine strings.Builder
		for i, char := range line {
			modifiedLine.WriteRune(char)
			if char == '(' {
				openCount++
			} else if char == ')' {
				openCount--
				for c, p := range parenthesesMap {
					if c == openCount {
						for i := 0; i < p; i++ {
							modifiedLine.WriteRune(')')
						}
						delete(parenthesesMap, c)
					}
				}
			} else if char == '!' {
				if macro, ok := IsMacro(line[i:]); ok && macro.IsNested() {
					parenthesesMap[openCount-1]++
				}
			}
		}
		modifiedLines = append(modifiedLines, modifiedLine.String())
	}
	var modifiedLine strings.Builder
	for c, p := range parenthesesMap {
		if c == -1 {
			for i := 0; i < p; i++ {
				modifiedLine.WriteRune(')')
			}
			delete(parenthesesMap, c)
		}
	}
	modifiedLines = append(modifiedLines, modifiedLine.String())
	lurkProgram = strings.Join(modifiedLines, "\n")

	if err := scanner.Err(); err != nil {
		return "", err
	}

	for _, macro := range []Macro{Def, Defrec, Defun, Assert, AssertEq, List, Param} {
		lurkProgram = macro.Expand(lurkProgram)
	}

	return lurkProgram, nil
}

func removeComments(expression string) string {
	var result strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(expression))

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(strings.TrimSpace(line), ";;") {
			result.WriteString(line + "\n")
		}
	}

	return result.String()
}
