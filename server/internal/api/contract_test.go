package api_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	apigen "github.com/ardam/navidrome-replacement/server/internal/api/gen"
	"github.com/ardam/navidrome-replacement/server/internal/testutil"
	"github.com/getkin/kin-openapi/openapi3"
)

// TestEmbeddedContractMatchesCommittedSpec proves the generated Go artifact
// is current: the spec embedded in the binary (and served at
// /api/openapi.yaml) must describe exactly the committed source of truth.
func TestEmbeddedContractMatchesCommittedSpec(t *testing.T) {
	sourcePath := filepath.Join("..", "..", "..", "packages", "contracts", "openapi.yaml")
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read committed contract: %v", err)
	}
	loader := openapi3.NewLoader()
	committed, err := loader.LoadFromData(source)
	if err != nil {
		t.Fatalf("parse committed contract: %v", err)
	}
	embedded, err := apigen.GetSwagger()
	if err != nil {
		t.Fatalf("decode embedded contract: %v", err)
	}

	if difference := firstDifference(canonicalJSON(t, committed), canonicalJSON(t, embedded), "$"); difference != "" {
		t.Fatalf("embedded OpenAPI spec differs from packages/contracts/openapi.yaml at %s; run `mise run generate`", difference)
	}
}

func firstDifference(committed, embedded any, path string) string {
	switch committedValue := committed.(type) {
	case map[string]any:
		embeddedValue, ok := embedded.(map[string]any)
		if !ok {
			return fmt.Sprintf("%s (committed object, embedded %T)", path, embedded)
		}
		keys := map[string]bool{}
		for key := range committedValue {
			keys[key] = true
		}
		for key := range embeddedValue {
			keys[key] = true
		}
		sortedKeys := make([]string, 0, len(keys))
		for key := range keys {
			sortedKeys = append(sortedKeys, key)
		}
		sort.Strings(sortedKeys)
		for _, key := range sortedKeys {
			left, inCommitted := committedValue[key]
			right, inEmbedded := embeddedValue[key]
			if inCommitted != inEmbedded {
				return fmt.Sprintf("%s.%s (committed present: %t, embedded present: %t)", path, key, inCommitted, inEmbedded)
			}
			if difference := firstDifference(left, right, path+"."+key); difference != "" {
				return difference
			}
		}
		return ""
	case []any:
		embeddedValue, ok := embedded.([]any)
		if !ok || len(embeddedValue) != len(committedValue) {
			return fmt.Sprintf("%s (committed array of %d, embedded %v)", path, len(committedValue), embedded)
		}
		for index := range committedValue {
			if difference := firstDifference(committedValue[index], embeddedValue[index], fmt.Sprintf("%s[%d]", path, index)); difference != "" {
				return difference
			}
		}
		return ""
	default:
		if !reflect.DeepEqual(committed, embedded) {
			return fmt.Sprintf("%s (committed %v, embedded %v)", path, committed, embedded)
		}
		return ""
	}
}

func TestCommittedContractIsValid(t *testing.T) {
	contract := testutil.Contract(t)
	if contract.OpenAPI == "" || len(contract.Paths.Map()) == 0 {
		t.Fatal("contract has no documented paths")
	}
	seen := map[string]string{}
	for path, item := range contract.Paths.Map() {
		for method, operation := range item.Operations() {
			if operation.OperationID == "" {
				t.Errorf("%s %s has no operationId; generated clients need one", method, path)
				continue
			}
			if previous, duplicate := seen[operation.OperationID]; duplicate {
				t.Errorf("operationId %q is used by both %s and %s %s", operation.OperationID, previous, method, path)
			}
			seen[operation.OperationID] = method + " " + path
		}
	}
}

func canonicalJSON(t *testing.T, document *openapi3.T) any {
	t.Helper()
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("encode contract: %v", err)
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode contract: %v", err)
	}
	return withoutGeneratorNormalizedFields(decoded)
}

// withoutGeneratorNormalizedFields drops the fields oapi-codegen rewrites
// while embedding (it exports operationIds by capitalizing them), so the
// comparison only reports genuine contract drift.
func withoutGeneratorNormalizedFields(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, entry := range typed {
			if key == "operationId" {
				continue
			}
			result[key] = withoutGeneratorNormalizedFields(entry)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, entry := range typed {
			result[index] = withoutGeneratorNormalizedFields(entry)
		}
		return result
	default:
		return value
	}
}
