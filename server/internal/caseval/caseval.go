// Package caseval validates case v1 documents (docs/contracts/case-v1.md) before they are stored.
package caseval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Limits from case-v1.md.
const (
	MaxBytes  = 64 * 1024
	MaxSteps  = 120 * 180 // 21 600
	MaxEvents = 200
	// DirTolerance is how far |dir| may be from 1. Clients round coordinates (the contract's own example,
	// [-0.19, -0.09, -0.98], has length 1.0023), so the tolerance allows two-decimal rounding.
	DirTolerance = 0.01
	// MaxCoord bounds shot origins, in metres from the world origin.
	MaxCoord = 1000
	// MaxBeats bounds a script's timeline (the simulator keeps at most 60 from the AI).
	MaxBeats  = 200
	MaxChecks = MaxSteps/1200 + 1
)

// Settings lists the allowed values for each setting key. bleed is accepted as a number or a string.
var Settings = map[string][]string{
	"weapon":  {"pistol", "rifle", "buck", "slug"},
	"impulse": {"real", "movie"},
	"react":   {"auto", "flee", "stay"},
	"bleed":   {"1", "10"},
}

// Error is a validation failure: where in the document and why, in Russian for the user.
type Error struct {
	Path   string
	Reason string
}

func (e *Error) Error() string {
	if e.Path == "" {
		return e.Reason
	}
	return e.Path + ": " + e.Reason
}

// Meta is what the server reads from case.meta for search and link previews.
type Meta struct {
	Title       string
	Description string
	Kind        string
	Weapon      string
	Zones       []string
	Organs      []string
}

// Summary is what the server stores in columns next to the case.
type Summary struct {
	Seed   int64
	Build  string
	Steps  int64
	Kind   string // meta.kind, may be empty
	Weapon string // meta.weapon, or else the first weapon setting; may be empty
	Meta   Meta
}

var (
	buildRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	slugRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	hashRe  = regexp.MustCompile(`^[0-9a-fA-F]{8}$`)
)

func fail(path, format string, a ...any) error {
	return &Error{Path: path, Reason: fmt.Sprintf(format, a...)}
}

// Validate checks raw (the case JSON) and returns the columns to store with it.
func Validate(raw []byte) (*Summary, error) {
	if len(raw) > MaxBytes {
		return nil, fail("", "кейс больше 64 КБ")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return nil, fail("", "кейс не является корректным JSON")
	}
	if dec.More() {
		return nil, fail("", "после кейса есть лишние данные")
	}
	top, ok := doc.(map[string]any)
	if !ok {
		return nil, fail("", "кейс должен быть объектом")
	}
	if err := onlyKeys("", top, "v", "seed", "build", "steps", "events", "check", "meta"); err != nil {
		return nil, err
	}

	s := &Summary{}
	v, err := intField(top, "", "v", true)
	if err != nil {
		return nil, err
	}
	if v != 1 {
		return nil, fail("v", "поддерживается только версия 1")
	}
	if s.Seed, err = intField(top, "", "seed", true); err != nil {
		return nil, err
	}
	if s.Seed < 0 || s.Seed > math.MaxUint32 {
		return nil, fail("seed", "должно быть целым от 0 до 4294967295")
	}
	build, err := strField(top, "", "build", true)
	if err != nil {
		return nil, err
	}
	if !buildRe.MatchString(build) {
		return nil, fail("build", "версия симулятора: от 1 до 64 латинских букв, цифр, «.», «_» или «-»")
	}
	s.Build = build
	if s.Steps, err = intField(top, "", "steps", true); err != nil {
		return nil, err
	}
	if s.Steps < 1 || s.Steps > MaxSteps {
		return nil, fail("steps", "должно быть от 1 до %d (не больше 180 с)", MaxSteps)
	}

	firstWeapon, err := validateEvents(top["events"], s.Steps)
	if err != nil {
		return nil, err
	}
	if c, ok := top["check"]; ok && c != nil {
		if err := validateChecks(c, s.Steps); err != nil {
			return nil, err
		}
	}
	if m, ok := top["meta"]; ok && m != nil {
		if s.Meta, err = validateMeta(m); err != nil {
			return nil, err
		}
	}
	s.Kind = s.Meta.Kind
	s.Weapon = s.Meta.Weapon
	if s.Weapon == "" {
		s.Weapon = firstWeapon
	}
	return s, nil
}

func validateEvents(raw any, steps int64) (firstWeapon string, err error) {
	if raw == nil {
		return "", fail("events", "обязательное поле")
	}
	list, ok := raw.([]any)
	if !ok {
		return "", fail("events", "должно быть массивом")
	}
	if len(list) > MaxEvents {
		return "", fail("events", "не больше %d событий", MaxEvents)
	}
	prev := int64(-1)
	for i, item := range list {
		path := fmt.Sprintf("events[%d]", i)
		ev, ok := item.(map[string]any)
		if !ok {
			return "", fail(path, "событие должно быть объектом")
		}
		step, err := intField(ev, path, "step", true)
		if err != nil {
			return "", err
		}
		if step < 0 || step > steps {
			return "", fail(path+".step", "должно быть от 0 до steps (%d)", steps)
		}
		if step < prev {
			return "", fail(path+".step", "события должны идти по возрастанию шага")
		}
		prev = step
		typ, err := strField(ev, path, "type", true)
		if err != nil {
			return "", err
		}
		switch typ {
		case "setting":
			key, value, err := validateSetting(ev, path)
			if err != nil {
				return "", err
			}
			if key == "weapon" && firstWeapon == "" {
				firstWeapon = value
			}
		case "shot":
			if err := validateShot(ev, path); err != nil {
				return "", err
			}
		case "script":
			if err := validateScript(ev, path); err != nil {
				return "", err
			}
		default:
			return "", fail(path+".type", "неизвестный тип события %q (setting, shot или script)", typ)
		}
	}
	return firstWeapon, nil
}

func validateSetting(ev map[string]any, path string) (key, value string, err error) {
	if err := onlyKeys(path, ev, "step", "type", "key", "value"); err != nil {
		return "", "", err
	}
	if key, err = strField(ev, path, "key", true); err != nil {
		return "", "", err
	}
	allowed, ok := Settings[key]
	if !ok {
		return "", "", fail(path+".key", "неизвестная настройка %q (weapon, impulse, react или bleed)", key)
	}
	switch v := ev["value"].(type) {
	case string:
		value = v
	case json.Number:
		if key != "bleed" {
			return "", "", fail(path+".value", "должно быть строкой")
		}
		value = v.String()
	case nil:
		return "", "", fail(path+".value", "обязательное поле")
	default:
		return "", "", fail(path+".value", "должно быть строкой")
	}
	for _, a := range allowed {
		if value == a {
			return key, value, nil
		}
	}
	return "", "", fail(path+".value", "для %s допустимо: %s", key, strings.Join(allowed, ", "))
}

func validateShot(ev map[string]any, path string) error {
	if err := onlyKeys(path, ev, "step", "type", "origin", "dir"); err != nil {
		return err
	}
	origin, err := vec3(ev, path, "origin")
	if err != nil {
		return err
	}
	for _, c := range origin {
		if math.Abs(c) > MaxCoord {
			return fail(path+".origin", "точка выстрела дальше %d м от центра сцены", MaxCoord)
		}
	}
	dir, err := vec3(ev, path, "dir")
	if err != nil {
		return err
	}
	l := math.Sqrt(dir[0]*dir[0] + dir[1]*dir[1] + dir[2]*dir[2])
	if math.Abs(l-1) > DirTolerance {
		return fail(path+".dir", "направление должно быть единичным вектором (длина %.4f)", l)
	}
	return nil
}

func validateScript(ev map[string]any, path string) error {
	if err := onlyKeys(path, ev, "step", "type", "source", "title", "beats", "temperament"); err != nil {
		return err
	}
	src, err := strField(ev, path, "source", true)
	if err != nil {
		return err
	}
	if src == "" || utf8.RuneCountInString(src) > 32 {
		return fail(path+".source", "от 1 до 32 символов")
	}
	title, err := strField(ev, path, "title", true)
	if err != nil {
		return err
	}
	if utf8.RuneCountInString(title) > 200 {
		return fail(path+".title", "не длиннее 200 символов")
	}
	if _, ok := ev["temperament"]; ok && ev["temperament"] != nil {
		t, err := strField(ev, path, "temperament", false)
		if err != nil {
			return err
		}
		if utf8.RuneCountInString(t) > 200 {
			return fail(path+".temperament", "не длиннее 200 символов")
		}
	}
	beats, ok := ev["beats"].([]any)
	if !ok {
		return fail(path+".beats", "должно быть массивом")
	}
	if len(beats) > MaxBeats {
		return fail(path+".beats", "не больше %d шагов сценария", MaxBeats)
	}
	for j, b := range beats {
		bp := fmt.Sprintf("%s.beats[%d]", path, j)
		beat, ok := b.(map[string]any)
		if !ok {
			return fail(bp, "должно быть объектом")
		}
		action, err := strField(beat, bp, "action", true)
		if err != nil {
			return err
		}
		if action == "" || len(action) > 32 {
			return fail(bp+".action", "от 1 до 32 символов")
		}
		for _, k := range []string{"t", "dur"} {
			if _, err := numField(beat, bp, k, true); err != nil {
				return err
			}
		}
		if _, ok := beat["intensity"]; ok {
			if _, err := numField(beat, bp, "intensity", false); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateChecks(raw any, steps int64) error {
	list, ok := raw.([]any)
	if !ok {
		return fail("check", "должно быть массивом")
	}
	if len(list) > MaxChecks {
		return fail("check", "не больше %d контрольных точек", MaxChecks)
	}
	for i, item := range list {
		path := fmt.Sprintf("check[%d]", i)
		c, ok := item.(map[string]any)
		if !ok {
			return fail(path, "должно быть объектом")
		}
		if err := onlyKeys(path, c, "step", "hash"); err != nil {
			return err
		}
		step, err := intField(c, path, "step", true)
		if err != nil {
			return err
		}
		if step < 0 || step > steps {
			return fail(path+".step", "должно быть от 0 до steps (%d)", steps)
		}
		h, err := strField(c, path, "hash", true)
		if err != nil {
			return err
		}
		if !hashRe.MatchString(h) {
			return fail(path+".hash", "8 шестнадцатеричных символов")
		}
	}
	return nil
}

func validateMeta(raw any) (Meta, error) {
	var m Meta
	obj, ok := raw.(map[string]any)
	if !ok {
		return m, fail("meta", "должно быть объектом")
	}
	var err error
	str := func(key string, max int) (string, error) {
		if v, ok := obj[key]; !ok || v == nil {
			return "", nil
		}
		s, err := strField(obj, "meta", key, false)
		if err != nil {
			return "", err
		}
		if utf8.RuneCountInString(s) > max {
			return "", fail("meta."+key, "не длиннее %d символов", max)
		}
		return s, nil
	}
	if m.Title, err = str("title", 200); err != nil {
		return m, err
	}
	if m.Description, err = str("description", 500); err != nil {
		return m, err
	}
	if m.Kind, err = str("kind", 64); err != nil {
		return m, err
	}
	if m.Kind != "" && !slugRe.MatchString(m.Kind) {
		return m, fail("meta.kind", "строчные латинские буквы, цифры, «-» и «_»")
	}
	if m.Weapon, err = str("weapon", 32); err != nil {
		return m, err
	}
	if m.Weapon != "" && !slugRe.MatchString(m.Weapon) {
		return m, fail("meta.weapon", "строчные латинские буквы, цифры, «-» и «_»")
	}
	if m.Zones, err = strList(obj, "zones"); err != nil {
		return m, err
	}
	if m.Organs, err = strList(obj, "organs"); err != nil {
		return m, err
	}
	return m, nil
}

func strList(obj map[string]any, key string) ([]string, error) {
	raw, ok := obj[key]
	if !ok || raw == nil {
		return nil, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, fail("meta."+key, "должно быть массивом строк")
	}
	if len(list) > 32 {
		return nil, fail("meta."+key, "не больше 32 элементов")
	}
	out := make([]string, 0, len(list))
	for i, it := range list {
		s, ok := it.(string)
		if !ok || utf8.RuneCountInString(s) > 100 {
			return nil, fail(fmt.Sprintf("meta.%s[%d]", key, i), "должно быть строкой не длиннее 100 символов")
		}
		out = append(out, s)
	}
	return out, nil
}

func onlyKeys(path string, obj map[string]any, allowed ...string) error {
	var extra []string
	for k := range obj {
		found := false
		for _, a := range allowed {
			if k == a {
				found = true
				break
			}
		}
		if !found {
			extra = append(extra, k)
		}
	}
	if len(extra) == 0 {
		return nil
	}
	sort.Strings(extra)
	return fail(path, "неизвестное поле %q", extra[0])
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func intField(obj map[string]any, path, key string, required bool) (int64, error) {
	raw, ok := obj[key]
	if !ok || raw == nil {
		if required {
			return 0, fail(join(path, key), "обязательное поле")
		}
		return 0, nil
	}
	n, ok := raw.(json.Number)
	if !ok {
		return 0, fail(join(path, key), "должно быть числом")
	}
	v, err := strconv.ParseInt(n.String(), 10, 64)
	if err != nil {
		return 0, fail(join(path, key), "должно быть целым числом")
	}
	return v, nil
}

func numField(obj map[string]any, path, key string, required bool) (float64, error) {
	raw, ok := obj[key]
	if !ok || raw == nil {
		if required {
			return 0, fail(join(path, key), "обязательное поле")
		}
		return 0, nil
	}
	n, ok := raw.(json.Number)
	if !ok {
		return 0, fail(join(path, key), "должно быть числом")
	}
	v, err := n.Float64()
	if err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
		return 0, fail(join(path, key), "должно быть конечным числом")
	}
	return v, nil
}

func strField(obj map[string]any, path, key string, required bool) (string, error) {
	raw, ok := obj[key]
	if !ok || raw == nil {
		if required {
			return "", fail(join(path, key), "обязательное поле")
		}
		return "", nil
	}
	s, ok := raw.(string)
	if !ok {
		return "", fail(join(path, key), "должно быть строкой")
	}
	return s, nil
}

func vec3(obj map[string]any, path, key string) ([3]float64, error) {
	var out [3]float64
	raw, ok := obj[key]
	if !ok || raw == nil {
		return out, fail(join(path, key), "обязательное поле")
	}
	list, ok := raw.([]any)
	if !ok || len(list) != 3 {
		return out, fail(join(path, key), "должно быть массивом из трёх чисел")
	}
	for i, it := range list {
		n, ok := it.(json.Number)
		if !ok {
			return out, fail(join(path, key), "должно быть массивом из трёх чисел")
		}
		v, err := n.Float64()
		if err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
			return out, fail(join(path, key), "должно быть массивом из трёх конечных чисел")
		}
		out[i] = v
	}
	return out, nil
}
