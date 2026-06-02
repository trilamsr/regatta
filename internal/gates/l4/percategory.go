package l4

import (
	"os"
	"sort"
	"strings"
)

// Spec §3.4 hunt-list categories, named constants so per-category
// overrides + tests reference the single source of truth.
const (
	CategoryCorrectness    = "correctness"
	CategoryDocCheck       = "doc-check"
	CategoryRefactor       = "refactor"
	CategoryRisk           = "risk"
	CategoryRubricVerify   = "rubric-verify"
	CategorySecurity       = "security"
	CategorySimplification = "simplification"
	CategoryTestCoverage   = "test-coverage"
)

// AllCategories is the spec §3.4 hunt list. Order is canonical so
// bucket-category slices sort stably for prompt rendering.
var AllCategories = []string{
	CategoryCorrectness,
	CategoryDocCheck,
	CategoryRefactor,
	CategoryRisk,
	CategoryRubricVerify,
	CategorySecurity,
	CategorySimplification,
	CategoryTestCoverage,
}

// EnvCategoryModelPrefix names the per-category env-var family.
// Suffix is the category name uppercased with `-` to `_`, so
// `security` reads from `REGATTA_GATES_L4_MODEL_SECURITY` and
// `doc-check` from `REGATTA_GATES_L4_MODEL_DOC_CHECK`.
const EnvCategoryModelPrefix = "REGATTA_GATES_L4_MODEL_"

// ResolveCategoryModel returns the model assigned to a category,
// honouring yaml > env > primary-fallback precedence. The primary
// model itself already resolved through ResolveModel — callers pass
// the resolved value, not the yaml string.
func ResolveCategoryModel(primary string, yamlCategoryModels map[string]string, category string) string {
	if v, ok := yamlCategoryModels[category]; ok && v != "" {
		return v
	}
	if env := os.Getenv(EnvCategoryModelPrefix + envSuffix(category)); env != "" {
		return env
	}
	return primary
}

// categoryBuckets groups categories by their resolved model. Output
// is keyed by model name; each value is the category slice sorted
// canonically. A single-bucket result means one Invoker call.
func categoryBuckets(primary string, yamlCategoryModels map[string]string) map[string][]string {
	buckets := map[string][]string{}
	for _, cat := range AllCategories {
		m := ResolveCategoryModel(primary, yamlCategoryModels, cat)
		buckets[m] = append(buckets[m], cat)
	}
	for m := range buckets {
		sort.Strings(buckets[m])
	}
	return buckets
}

// bucketModelsSorted returns bucket model names in deterministic
// order (primary first when present, then alphabetical) so call-site
// ordering is stable across runs for telemetry replay.
func bucketModelsSorted(buckets map[string][]string, primary string) []string {
	out := make([]string, 0, len(buckets))
	for m := range buckets {
		if m == primary {
			continue
		}
		out = append(out, m)
	}
	sort.Strings(out)
	if _, ok := buckets[primary]; ok {
		out = append([]string{primary}, out...)
	}
	return out
}

func envSuffix(category string) string {
	return strings.ToUpper(strings.ReplaceAll(category, "-", "_"))
}
