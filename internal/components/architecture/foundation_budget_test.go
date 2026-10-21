package architecture

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// These budgets gate new authority-bearing syntax for owner review. They do
// not label every SQL call or worker a defect. R1's lexical observations stay
// advisory; this inventory uses parsed imports, declarations and expressions.
type foundationSite struct {
	Rule   string `json:"rule"`
	Path   string `json:"path"`
	Symbol string `json:"symbol"`
	Digest string `json:"digest"`
}

type foundationBudget struct {
	BaseSHA string           `json:"baseSHA"`
	Sites   []foundationSite `json:"sites"`
}

func TestFoundationRegressionBudget(t *testing.T) {
	root := moduleRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "internal/components/architecture/foundation-budget.json"))
	if err != nil {
		t.Fatal(err)
	}
	var budget foundationBudget
	if err := json.Unmarshal(data, &budget); err != nil {
		t.Fatal(err)
	}
	if budget.BaseSHA != "bb306959fe17f49bfcb201cb69eb39cf7f7e4030" {
		t.Fatal("foundation budget must retain its reviewed R1 source identity")
	}
	sites, forbidden := foundationInventory(t, root)
	for _, violation := range forbidden {
		t.Error(violation)
	}
	for _, site := range addedFoundationSites(budget.Sites, sites) {
		t.Errorf("unreviewed %s at %s:%s (%s); consume the semantic owner port or provide an explicit owner/lifetime review; do not regenerate the legacy budget", site.Rule, site.Path, site.Symbol, site.Digest)
	}
}

func foundationInventory(t *testing.T, root string) ([]foundationSite, []string) {
	t.Helper()
	var sites []foundationSite
	var forbidden []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if (path != root && strings.HasPrefix(entry.Name(), ".")) || skipDir(entry.Name()) || entry.Name() == "testdata" || entry.Name() == "vendor" || entry.Name() == "testsupport" || entry.Name() == "testutil" || entry.Name() == "tests" || entry.Name() == "fixtures" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(mustRel(t, root, path))
		found, rejected := inspectFoundationFile(rel, file)
		sites = append(sites, found...)
		forbidden = append(forbidden, rejected...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sortFoundationSites(sites)
	sort.Strings(forbidden)
	return sites, forbidden
}

func inspectFoundationFile(path string, file *ast.File) ([]foundationSite, []string) {
	imports := map[string]string{}
	var sites []foundationSite
	var forbidden []string
	for _, spec := range file.Imports {
		imported, _ := strconv.Unquote(spec.Path.Value)
		alias := filepath.Base(imported)
		if spec.Name != nil {
			alias = spec.Name.Name
		}
		imports[alias] = imported
		if typedComponentComposition(path) &&
			(imported == modulePath+"/database/sqlite" || imported == "gorm.io/gorm") {
			forbidden = append(forbidden, path+": typed Component composition must use its declared owner ports, not raw database access")
		}
		if imported == modulePath+"/service" && alias == "." {
			forbidden = append(forbidden, path+": dot import hides broad Runtime authority")
		}
		if imported == modulePath+"/network/nftables" && alias == "." && strings.HasPrefix(path, "components/") {
			forbidden = append(forbidden, path+": dot import hides privileged network adapter construction")
		}
		if strings.HasPrefix(imported, modulePath+"/components/") && !componentImportAllowed(path, imported) {
			forbidden = append(forbidden, path+": foreign component implementation import "+imported)
		}
		if imported == "os/exec" && !privilegedExecutionOwner(filepath.ToSlash(filepath.Dir(path))) {
			forbidden = append(forbidden, path+": raw OS execution outside an accepted semantic owner")
		}
	}
	add := func(rule, symbol string, node ast.Node) {
		var out bytes.Buffer
		if err := format.Node(&out, token.NewFileSet(), node); err != nil {
			panic(err)
		}
		digest := sha256.Sum256(out.Bytes())
		sites = append(sites, foundationSite{rule, path, symbol, hex.EncodeToString(digest[:])})
	}
	for _, declaration := range file.Decls {
		symbol := ""
		switch decl := declaration.(type) {
		case *ast.FuncDecl:
			symbol = decl.Name.Name
			if decl.Recv != nil {
				var out bytes.Buffer
				_ = format.Node(&out, token.NewFileSet(), decl.Recv.List[0].Type)
				symbol = out.String() + "." + symbol
			}
		case *ast.GenDecl:
			// Inspect each spec independently, so an unrelated declaration in
			// a const/type/var block does not inherit another site's budget.
			for _, spec := range decl.Specs {
				name := ""
				switch value := spec.(type) {
				case *ast.TypeSpec:
					name = value.Name.Name
				case *ast.ValueSpec:
					for _, identifier := range value.Names {
						name += identifier.Name + ","
					}
				}
				inspectFoundationDeclaration(path, name, spec, imports, add, &forbidden)
			}
			continue
		default:
			continue
		}
		inspectFoundationDeclaration(path, symbol, declaration, imports, add, &forbidden)
	}
	return sites, forbidden
}

func inspectFoundationDeclaration(path, symbol string, declaration ast.Node, imports map[string]string, add func(string, string, ast.Node), forbidden *[]string) {
	component := strings.HasPrefix(path, "components/")
	coreModel := false
	databaseAdapter := false
	for _, imported := range imports {
		coreModel = coreModel || imported == modulePath+"/database/model"
		databaseAdapter = databaseAdapter || imported == "gorm.io/gorm" || imported == modulePath+"/database/sqlite"
	}
	ast.Inspect(declaration, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.SelectorExpr:
			if qualifier, ok := value.X.(*ast.Ident); ok && component && imports[qualifier.Name] == modulePath+"/network/nftables" &&
				(value.Sel.Name == "NewEngine" || value.Sel.Name == "NewEngineWithExecutor" || value.Sel.Name == "RemoveManagedTableForPackageRemoval") && filepath.ToSlash(filepath.Dir(path)) != "components/server-protection/service/helper" {
				*forbidden = append(*forbidden, path+":"+symbol+": privileged network adapter entrypoint belongs to the typed broker bridge")
			}
			if typedComponentComposition(path) {
				qualifier, imported := value.X.(*ast.Ident)
				coreService := imported && imports[qualifier.Name] == modulePath+"/service"
				if value.Sel.Name == "Runtime" || coreService && (value.Sel.Name == "DefaultRuntime" || value.Sel.Name == "SetDefaultRuntime" || strings.HasPrefix(value.Sel.Name, "NewRuntime")) {
					*forbidden = append(*forbidden, path+":"+symbol+": typed Component composition must use declared owner ports, not broad Runtime")
				}
			}
			if qualifier, ok := value.X.(*ast.Ident); ok && value.Sel.Name == "Runtime" && imports[qualifier.Name] == modulePath+"/service" {
				add("broad-runtime", symbol, value)
			}
		case *ast.GoStmt:
			// Digest the full launch expression, including a closure body.
			// A new/changed launch requires cancellation/bounds/drain review.
			add("worker-lifetime", symbol, value)
		case *ast.CallExpr:
			selector, ok := value.Fun.(*ast.SelectorExpr)
			if !ok {
				break
			}
			if qualifier, ok := selector.X.(*ast.Ident); ok && imports[qualifier.Name] == modulePath+"/componenthost/installstate" && selector.Sel.Name == "SetInstalled" && strings.HasPrefix(path, "components/panel-update-ui/service/") {
				var installed *ast.Ident
				var literal bool
				if len(value.Args) == 4 {
					installed, literal = value.Args[3].(*ast.Ident)
				}
				guarded := path == "components/panel-update-ui/service/removal.go" && symbol == "RuntimeManager.commitMetadataRemoval"
				if !guarded && (!literal || installed.Name != "true") {
					*forbidden = append(*forbidden, path+":"+symbol+": component metadata removal must use safe owner admission")
				}
			}
			if qualifier, ok := selector.X.(*ast.Ident); ok && imports[qualifier.Name] == "time" && (selector.Sel.Name == "NewTicker" || selector.Sel.Name == "Tick") {
				add("worker-lifetime", symbol, value)
			}
			// Database adapters can own legitimate writes too.
			// Budget their exact mutation expressions for owner review rather
			// than pretending a method-name match proves a foreign write.
			if component && (coreModel || databaseAdapter || hasTableSelector(value)) && databaseMutationMethod(selector.Sel.Name) && !semanticOutboundOwnerSave(selector, imports) {
				add("component-model-mutation", symbol, value)
			}
		case *ast.TypeSpec:
			if shadowNodeFeature(value.Name.Name) {
				*forbidden = append(*forbidden, path+":"+symbol+": shadow Node feature configuration")
			}
			if strings.Contains(value.Name.Name, "Request") && rootRPCPath(path) {
				ast.Inspect(value.Type, func(child ast.Node) bool {
					field, ok := child.(*ast.Field)
					if ok && field.Tag != nil {
						tag, _ := strconv.Unquote(field.Tag.Value)
						name := strings.Split(reflect.StructTag(tag).Get("json"), ",")[0]
						if genericRootField(name) {
							*forbidden = append(*forbidden, path+":"+symbol+": generic root RPC field "+name)
						}
					}
					return true
				})
			}
		case *ast.BasicLit:
			if value.Kind == token.STRING {
				literal, _ := strconv.Unquote(value.Value)
				if literal == "direct" {
					// Explicit Direct configurations are legitimate. New sites
					// still need a policy/omission review before budget admission.
					add("direct-policy", symbol, declaration)
				}
			}
		case *ast.Ident:
			if value.Name == "FailoverFinalDirect" {
				add("direct-policy", symbol, declaration)
			}
		}
		return true
	})
}

func hasTableSelector(node ast.Node) bool {
	found := false
	ast.Inspect(node, func(child ast.Node) bool {
		if call, ok := child.(*ast.CallExpr); ok {
			if selector, ok := call.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "Table" {
				found = true
			}
		}
		return true
	})
	return found
}

func databaseMutationMethod(name string) bool {
	switch name {
	case "Create", "CreateInBatches", "Save", "Update", "Updates", "UpdateColumn", "UpdateColumns", "Delete", "Exec", "AutoMigrate":
		return true
	}
	return false
}

func privilegedExecutionOwner(path string) bool {
	switch path {
	case "cmd/solovey-openwrt-lifecycle", "components/server-protection/service/helper", "network/nftables", "internal/ops/deploymentbroker", "internal/ops/privilegedbroker", "internal/ops/sshbroker", "internal/ops/updatebroker":
		return true
	}
	return false
}

func rootRPCPath(path string) bool {
	return strings.HasPrefix(path, "internal/ops/") || strings.HasPrefix(path, "components/server-protection/service/helper/")
}

func genericRootField(name string) bool {
	switch name {
	case "command", "argv", "shell", "environment", "env", "serviceName":
		return true
	}
	return false
}

func shadowNodeFeature(name string) bool {
	switch name {
	case "NodeRouting", "NodeDNS", "NodeInboundConfig", "NodeOutboundConfig":
		return true
	}
	return false
}

func sortFoundationSites(sites []foundationSite) {
	sort.Slice(sites, func(i, j int) bool { return foundationSiteKey(sites[i]) < foundationSiteKey(sites[j]) })
}

func foundationSiteKey(site foundationSite) string {
	return fmt.Sprintf("%s\x00%s\x00%s\x00%s", site.Rule, site.Path, site.Symbol, site.Digest)
}

func addedFoundationSites(baseline, current []foundationSite) []foundationSite {
	counts := map[string]int{}
	for _, site := range baseline {
		counts[foundationSiteKey(site)]++
	}
	var added []foundationSite
	for _, site := range current {
		key := foundationSiteKey(site)
		if counts[key] > 0 {
			counts[key]--
		} else {
			added = append(added, site)
		}
	}
	sortFoundationSites(added)
	return added
}

func TestFoundationProhibitedChanges(t *testing.T) {
	for _, fixture := range []struct{ name, path, source, rule string }{
		{"runtime", "components/demo/adapter.go", `package demo; import owner "` + modulePath + `/service"; type Adapter struct { Runtime *owner.Runtime }`, "broad-runtime"},
		{"worker", "components/demo/worker.go", `package demo; func Start() { go func() { for {} }() }`, "worker-lifetime"},
		{"mutation", "components/demo/write.go", `package demo; import "` + modulePath + `/database/model"; func Write() { tx.Save(&model.Client{}) }`, "component-model-mutation"},
		{"table-mutation", "components/demo/write.go", `package demo; func Write() { tx.Table("clients").Delete(nil) }`, "component-model-mutation"},
		{"sql-mutation", "components/demo/write.go", `package demo; import "gorm.io/gorm"; func Write(tx *gorm.DB) { tx.Exec("UPDATE clients SET enable = true") }`, "component-model-mutation"},
		{"direct", "internal/entities/outbounds/new.go", `package outbounds; func final() string { return "direct" }`, "direct-policy"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), fixture.path, fixture.source, 0)
			if err != nil {
				t.Fatal(err)
			}
			sites, _ := inspectFoundationFile(fixture.path, file)
			added := addedFoundationSites(nil, sites)
			if len(added) != 1 || added[0].Rule != fixture.rule {
				t.Fatalf("expected %s review rejection, got %+v", fixture.rule, added)
			}
		})
	}
	for _, fixture := range []struct{ name, path, source string }{
		{"removal-bypass", "components/panel-update-ui/service/runtime.go", `package service; import owner "` + modulePath + `/componenthost/installstate"; func Remove() { owner.SetInstalled(path, manifests, id, false) }`},
		{"removal-boolean-compensation", "components/panel-update-ui/service/runtime.go", `package service; import owner "` + modulePath + `/componenthost/installstate"; func compensate(installed bool) { owner.SetInstalled(path, manifests, id, installed) }`},
		{"telegram-host-runtime", "components/telegram/adapter.go", `package telegram; func build(host APIDeps) { use(host.Runtime) }`},
		{"telegram-lifecycle-runtime", "components/telegram/adapter.go", `package telegram; func start(ctx Context) { use(ctx.Host.API.Runtime) }`},
		{"telegram-global-runtime", "components/telegram/adapter.go", `package telegram; import owner "` + modulePath + `/service"; func build() { use(owner.DefaultRuntime()) }`},
		{"sibling", "components/a/adapter.go", `package a; import "` + modulePath + `/components/b/internal/store"`},
		{"exec", "components/a/adapter.go", `package a; import run "os/exec"; func Start() { run.Command("sh") }`},
		{"rpc", "internal/ops/privilegedbroker/extra.go", "package broker; type ExtraRequest struct { Command string `json:\"command\"` }"},
		{"node", "components/nodes/config.go", `package nodes; type NodeRouting struct { Rules []string }`},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), fixture.path, fixture.source, 0)
			if err != nil {
				t.Fatal(err)
			}
			_, rejected := inspectFoundationFile(fixture.path, file)
			if len(rejected) == 0 {
				t.Fatal("prohibited boundary was accepted")
			}
		})
	}
}

func TestFoundationLegitimateOwnersAndDeterministicBudget(t *testing.T) {
	telegramPorts, err := parser.ParseFile(token.NewFileSet(), "ports.go", `package telegram; func build(host APIDeps) { use(host.OutboundHTTP); use(host.Audit.Recorder) }`, 0)
	if err != nil {
		t.Fatal(err)
	}
	if sites, rejected := inspectFoundationFile("components/telegram/adapter.go", telegramPorts); len(sites) != 0 || len(rejected) != 0 {
		t.Fatalf("legitimate narrow Telegram ports rejected: %v %v", sites, rejected)
	}
	// Native exec is legitimate within the reviewed semantic owner. An
	// observed procd command is a fact, not a generic Request escape hatch.
	source := "package broker; import \"os/exec\"; type ProcessFact struct { Command []string `json:\"command\"` }; func Inspect() { exec.Command(\"/usr/bin/systemctl\", \"show\") }"
	file, err := parser.ParseFile(token.NewFileSet(), "owner.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	sites, rejected := inspectFoundationFile("internal/ops/deploymentbroker/owner.go", file)
	if len(sites) != 0 || len(rejected) != 0 {
		t.Fatalf("legitimate owner/fact rejected: %v %v", sites, rejected)
	}
	a := foundationSite{Rule: "worker-lifetime", Path: "a.go", Symbol: "Start", Digest: "one"}
	b := foundationSite{Rule: "broad-runtime", Path: "b.go", Symbol: "Adapter", Digest: "two"}
	if added := addedFoundationSites([]foundationSite{a, b}, []foundationSite{b}); len(added) != 0 {
		t.Fatal("debt reduction rejected")
	}
	if added := addedFoundationSites([]foundationSite{a}, []foundationSite{a, a}); len(added) != 1 {
		t.Fatal("duplicate launch borrowed an existing budget")
	}
	left := addedFoundationSites(nil, []foundationSite{a, b})
	right := addedFoundationSites(nil, []foundationSite{b, a})
	if !reflect.DeepEqual(left, right) {
		t.Fatal("budget delta depends on traversal order")
	}
	parseSites := func(source string) []foundationSite {
		file, err := parser.ParseFile(token.NewFileSet(), "policy.go", source, 0)
		if err != nil {
			t.Fatal(err)
		}
		sites, _ := inspectFoundationFile("internal/entities/outbounds/policy.go", file)
		return sites
	}
	before := parseSites("package outbounds; func final() string { return \"direct\" }")
	afterFormatting := parseSites("package outbounds\r\n// a comment\r\nfunc final() string {\r\n return \"direct\"\r\n}\r\n")
	if !reflect.DeepEqual(before, afterFormatting) {
		t.Fatal("AST budget depends on comments, formatting or line endings")
	}
	changed := parseSites("package outbounds; func final() string { if missing { return \"direct\" }; return target }")
	if len(addedFoundationSites(before, changed)) != 1 {
		t.Fatal("an existing explicit Direct site concealed a new fallback")
	}
}

func typedComponentComposition(path string) bool {
	directory := filepath.ToSlash(filepath.Dir(path))
	return directory == "components/telegram" || directory == "components/observability-extra"
}

// An unshadowed package-qualified call to this existing semantic owner is not
// direct SQL. Other Save receivers and local shadows retain the mutation gate.
func semanticOutboundOwnerSave(selector *ast.SelectorExpr, imports map[string]string) bool {
	qualifier, ok := selector.X.(*ast.Ident)
	return ok && qualifier.Obj == nil && selector.Sel.Name == "Save" &&
		imports[qualifier.Name] == modulePath+"/internal/entities/outbounds"
}
