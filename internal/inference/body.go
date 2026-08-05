package inference

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"net/http" // for StatusOK/StatusText only — HTTP protocol semantics, not a framework dependency (those live in dialect_nethttp.go)
	"strconv"

	"github.com/pabloos/gota/internal/astutil"
	"github.com/pabloos/gota/internal/extractor"
	"github.com/pabloos/gota/pkg/model"
)

// DetectBody is a best-effort heuristic that walks decl's body looking
// for the request/response idioms d recognizes (see Dialect; the call
// shapes themselves — e.g. netHTTPDialect's Decode/Encode/WriteHeader/
// http.Error set — live with each dialect, not here) and, when found,
// augments op with a requestBody and/or response schemas. A chain of
// helper functions wrapping a recognized call — same-package or not —
// is followed as if inlined, see "Following indirection" below.
//
// A nil d disables detection entirely (the operation is still
// documented from routes and "gota:" comments, just without inferred
// bodies) — the honest degrade for a router plugin with no paired
// dialect.
//
// The detected schema is a bare "$ref: '#/components/schemas/<Name>'" (or
// an array of one) using the same convention a hand-written "gota:"
// comment would use, so the ResolveSchemaRefs pass expands it into a real
// component the same way regardless of which source produced it.
//
// Response detection walks the body respecting if/else branch boundaries:
// the status code in effect for a given Encode/Marshal/http.Error call is
// whatever the *nearest enclosing branch* last set via WriteHeader (or 200,
// Go's implicit default, if none did) — a WriteHeader in one branch never
// leaks into a sibling branch. Distinct status codes coexist as separate
// responses (e.g. a 200 success path and a 404 error path both get
// documented); if the *same* code is produced more than once, the last
// occurrence in source order wins.
//
// This is not a general dataflow analysis, and a WriteHeader call whose
// argument isn't a compile-time constant is ignored (the code in effect
// is left unchanged) rather than guessed. Detecting nothing is silent,
// not an error: a "gota:" comment remains the reliable, explicit path,
// and always wins over whatever this infers.
//
// # Following indirection
//
// funcIndex (typically internal/astutil.IndexFuncDecls over every loaded
// package) lets detection follow a call to a helper function — in the
// same package or a different one — that itself makes a call the
// dialect recognizes, or delegates further to another helper, up to
// maxFollowDepth calls deep. A parameter reference inside
// a followed helper's body resolves back to whatever expression was
// actually passed at its own call site, transitively through as many
// levels as it takes — e.g.:
//
//	// package httputil
//	func Success(w http.ResponseWriter, status int, data any) {
//		JSON(w, status, map[string]any{"status": "success", "data": data})
//	}
//	func JSON(w http.ResponseWriter, status int, payload any) {
//		w.WriteHeader(status)
//		json.NewEncoder(w).Encode(payload)
//	}
//
//	// package products
//	httputil.Success(w, http.StatusCreated, product)
//
// resolves all the way through: JSON's "payload" is Success's map
// literal, and "data" inside that literal is Success's own parameter,
// which resolves again to products' own "product" — two packages, two
// levels of helper, one detected 201 response with a real schema.
//
// Followable helpers include functions and methods alike, as long as
// their declaration is in the analyzed module (that's what funcIndex
// spans) — a server struct's own s.respond(...) is followed the same
// as a free function, while a call outside the module (a stdlib or
// framework call) or through a function-typed variable declines.
// Declining to follow (silent, not an error) also happens for: a
// helper with a variadic parameter; an argument count that doesn't
// match the helper's parameter count; a chain already maxFollowDepth
// calls deep; and a helper already present earlier in the current
// chain (a direct or mutual cycle — declined immediately, not just
// eventually stopped by the depth cap, since nothing here does real
// dataflow analysis and an unbounded static call graph walk has no
// other way to terminate on a recursive helper). Pass a nil funcIndex
// to disable following entirely.
//
// cmap, if non-nil, lets a specific statement opt out of response
// detection entirely via a "gota:" comment declaring "x-gota-skip: true"
// placed directly above it — e.g. an immature error path the developer
// doesn't want documented yet, without affecting the rest of the
// handler's detected responses. This only applies to response
// detection, not request body detection, and only to statements in
// decl's own body — a "gota:" comment inside a followed helper's body
// has no effect (cmap is built from decl's own file, and a shared
// helper has no single caller to scope a skip to anyway).
func DetectBody(op *model.Operation, decl *ast.FuncDecl, info *types.Info, cmap ast.CommentMap, funcIndex map[types.Object]astutil.FuncDeclInfo, d Dialect, ambiguous map[string]bool) {
	DetectBodyWithRoots(op, decl, info, cmap, funcIndex, d, ambiguous, nil)
}

// DetectBodyWithRoots is DetectBody plus roots — the set of analyzed root
// package import paths (see RootPaths). A detected response/request type
// declared OUTSIDE those roots (a dependency, or another go.work module)
// is package-qualified in its component name (componentName), so it
// resolves uniquely by its own package instead of colliding on a bare
// name with an unrelated same-named type elsewhere in the reachable graph.
// generate.Run passes roots; DetectBody passes nil, meaning "treat every
// type as a root" (bare names) — the correct standalone default when the
// caller doesn't distinguish roots from dependencies.
func DetectBodyWithRoots(op *model.Operation, decl *ast.FuncDecl, info *types.Info, cmap ast.CommentMap, funcIndex map[types.Object]astutil.FuncDeclInfo, d Dialect, ambiguous, roots map[string]bool) {
	if op == nil || decl == nil || decl.Body == nil || info == nil || d == nil {
		return
	}
	ctx := &evalCtx{info: info, funcIndex: funcIndex, visiting: map[types.Object]bool{}, dialect: d, ambiguous: ambiguous, roots: roots}

	// When decl is a handler factory — func(...) http.Handler returning an
	// inline handler, possibly wrapped in middleware — the request/response
	// idioms live in the returned literal, not decl's own statements.
	body := handlerBody(decl, info)

	schema, ok := firstBodySchema(body, ctx)
	if !ok {
		// No decode of the body in the handler itself — but a factory may
		// bind it in a generic middleware (Chain(BindJSON[Req])(...)),
		// whose type argument names the body type.
		schema, ok = middlewareBodySchema(decl, info, ambiguous, roots)
	}
	if ok {
		op.RequestBody = &model.RequestBody{
			Required: true,
			Content:  map[string]model.MediaType{"application/json": {Schema: schema}},
		}
	}

	rc := &responseCollector{responses: map[string]model.Response{}}
	rc.walk(body.List, http.StatusOK, ctx, cmap)
	if len(rc.responses) > 0 {
		if op.Responses == nil {
			op.Responses = map[string]model.Response{}
		}
		for code, resp := range rc.responses {
			op.Responses[code] = resp
		}
	}
}

// handlerBody returns the block holding the actual handler logic. Usually
// that's decl's own body, but when decl is a handler FACTORY — a
// func(...) http.Handler that returns an inline handler, on its own
// (return http.HandlerFunc(func(w, r){...})) or wrapped in middleware
// (return Chain(mw...)(http.HandlerFunc(func(w, r){...}))) — the
// request/response idioms are inside that returned literal, not decl's
// top-level statements, so this descends to it. The wrapper is unwrapped
// type-aware, following the one http.Handler-shaped argument at each
// layer down to the literal.
func handlerBody(decl *ast.FuncDecl, info *types.Info) *ast.BlockStmt {
	if !funcReturnsHTTPHandler(decl.Type, info) {
		return decl.Body
	}
	var lit *ast.FuncLit
	ast.Inspect(decl.Body, func(n ast.Node) bool {
		if lit != nil {
			return false
		}
		ret, ok := n.(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			return true
		}
		lit = unwrapReturnedHandlerLit(ret.Results[0], info)
		return lit == nil
	})
	if lit != nil {
		return lit.Body
	}
	return decl.Body
}

// middlewareBodySchema infers the request body of a handler factory from
// a generic middleware in its returned chain whose type argument names a
// struct — the common "bind-and-validate" idiom, Chain(BindJSON[Req])(
// handler), where In is the decoded body even though the handler never
// decodes it itself. It scans only the factory's returned expression (the
// middleware chain), and only a type ARGUMENT that is a named struct
// counts, so ordinary value indexing (arr[i]) and non-struct type
// parameters (Cache[string]) are ignored. Best-effort, like all body
// inference: a "gota:" comment always overrides it.
func middlewareBodySchema(decl *ast.FuncDecl, info *types.Info, ambiguous, roots map[string]bool) (*model.Schema, bool) {
	if !funcReturnsHTTPHandler(decl.Type, info) {
		return nil, false
	}
	var schema *model.Schema
	ast.Inspect(decl.Body, func(n ast.Node) bool {
		if schema != nil {
			return false
		}
		ret, ok := n.(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			return true
		}
		ast.Inspect(ret.Results[0], func(m ast.Node) bool {
			if schema != nil {
				return false
			}
			var typeArgs []ast.Expr
			switch x := m.(type) {
			case *ast.IndexExpr:
				typeArgs = []ast.Expr{x.Index}
			case *ast.IndexListExpr:
				typeArgs = x.Indices
			default:
				return true
			}
			for _, ta := range typeArgs {
				if tv, ok := info.Types[ta]; !ok || !tv.IsType() {
					continue // a value index (arr[i]), not a type argument
				}
				if s, ok := shallowRefSchema(info.TypeOf(ta), ambiguous, roots); ok && isNamedStructType(info.TypeOf(ta)) {
					schema = s
					return false
				}
			}
			return true
		})
		return false // scan only the first return's expression
	})
	return schema, schema != nil
}

// isNamedStructType reports whether t is a named struct type (optionally
// behind a pointer) — the shape middlewareBodySchema accepts as a body.
func isNamedStructType(t types.Type) bool {
	if t == nil {
		return false
	}
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	named, ok := t.(*types.Named)
	if !ok {
		return false
	}
	_, ok = named.Underlying().(*types.Struct)
	return ok
}

// funcReturnsHTTPHandler reports whether ft is a factory signature —
// exactly one result whose type is http.Handler-shaped.
func funcReturnsHTTPHandler(ft *ast.FuncType, info *types.Info) bool {
	if ft.Results == nil || len(ft.Results.List) != 1 {
		return false
	}
	return isHTTPHandlerType(info.TypeOf(ft.Results.List[0].Type))
}

// unwrapReturnedHandlerLit follows a returned handler expression down to
// the inline func literal it ultimately wraps: an http.HandlerFunc(lit)
// conversion, or a middleware call whose single http.Handler-shaped
// argument is the (possibly further-wrapped) handler. Returns nil when no
// literal is reached (e.g. the factory returns a named handler).
func unwrapReturnedHandlerLit(e ast.Expr, info *types.Info) *ast.FuncLit {
	for {
		switch x := e.(type) {
		case *ast.ParenExpr:
			e = x.X
		case *ast.FuncLit:
			return x
		case *ast.CallExpr:
			arg := singleHTTPHandlerArg(x, info)
			if arg == nil {
				return nil
			}
			e = arg
		default:
			return nil
		}
	}
}

// singleHTTPHandlerArg returns call's sole http.Handler-shaped argument,
// or nil unless there's exactly one (so a middleware wrapper's handler
// argument is followed, while its config arguments are ignored).
func singleHTTPHandlerArg(call *ast.CallExpr, info *types.Info) ast.Expr {
	var found ast.Expr
	n := 0
	for _, a := range call.Args {
		if isHTTPHandlerType(info.TypeOf(a)) {
			found = a
			n++
		}
	}
	if n == 1 {
		return found
	}
	return nil
}

// isHTTPHandlerType reports whether t is an http.Handler, an
// http.HandlerFunc, or a bare func(http.ResponseWriter, *http.Request).
func isHTTPHandlerType(t types.Type) bool {
	if t == nil {
		return false
	}
	if named, ok := t.(*types.Named); ok {
		if o := named.Obj(); o.Pkg() != nil && o.Pkg().Path() == "net/http" &&
			(o.Name() == "Handler" || o.Name() == "HandlerFunc") {
			return true
		}
	}
	sig, ok := t.Underlying().(*types.Signature)
	if !ok || sig.Params().Len() != 2 || sig.Results().Len() != 0 {
		return false
	}
	p0, ok := sig.Params().At(0).Type().(*types.Named)
	if !ok || p0.Obj().Pkg() == nil || p0.Obj().Pkg().Path() != "net/http" || p0.Obj().Name() != "ResponseWriter" {
		return false
	}
	p1, ok := sig.Params().At(1).Type().(*types.Pointer)
	if !ok {
		return false
	}
	named, ok := p1.Elem().(*types.Named)
	return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "net/http" && named.Obj().Name() == "Request"
}

// maxFollowDepth caps how many calls deep DetectBody will follow to find
// a WriteHeader/Encode/Decode call — generous for realistic wrapper
// chains (the deepest real-world case found so far is 2) without being
// unbounded; combined with cycle detection (evalCtx.visiting) so a
// self- or mutually-recursive helper can't cause runaway recursion.
const maxFollowDepth = 4

// evalCtx carries what every extraction function below needs to resolve
// an expression to a type or compile-time constant, including — once
// walking inside a followed helper (see tryFollow) — how to translate
// the helper's own parameter references back to the expressions actually
// passed at its call site, however many levels up that call site is and
// whichever package it's in.
type evalCtx struct {
	info *types.Info
	// subst maps a followed helper's own parameter objects to the
	// expression passed for them at the call site, *plus* the context
	// needed to interpret that expression (which may itself be another
	// helper's parameter reference, resolved under a still-earlier
	// context — see resolveExpr). nil at the top level (decl's own body).
	subst     map[*types.Var]boundExpr
	funcIndex map[types.Object]astutil.FuncDeclInfo
	depth     int                   // 0 at the top level, incremented by one per follow
	visiting  map[types.Object]bool // every helper already in the current follow chain, for cycle detection
	dialect   Dialect               // the recognizer set in effect, constant across the whole walk (followed frames inherit it)
	ambiguous map[string]bool       // schema names to package-qualify (see AmbiguousSchemaNames), constant across the walk
	roots     map[string]bool       // analyzed root package import paths; a type outside them is a dependency, package-qualified (see componentName), constant across the walk
}

// boundExpr is a followed helper's parameter binding: the expression
// passed for it at the call site, and the context that expression must
// be interpreted under (the call site's own context, not the helper's).
type boundExpr struct {
	expr ast.Expr
	ctx  *evalCtx
}

// resolveExpr returns the expression e ultimately stands for, plus the
// context needed to interpret it. If e is a bare identifier bound in
// ctx.subst, it keeps resolving through as many bound expressions as it
// takes — a followed helper's own parameter can itself be bound to
// another followed helper's parameter reference, arbitrarily many levels
// up the follow chain — stopping as soon as the result isn't a bound
// identifier under its current context. Returns e and ctx unchanged
// (the common case) when no substitution applies at all.
func resolveExpr(e ast.Expr, ctx *evalCtx) (ast.Expr, *evalCtx) {
	for {
		ident, ok := e.(*ast.Ident)
		if !ok {
			return e, ctx
		}
		v, ok := ctx.info.Uses[ident].(*types.Var)
		if !ok {
			return e, ctx
		}
		bound, ok := ctx.subst[v]
		if !ok {
			return e, ctx
		}
		e, ctx = bound.expr, bound.ctx
	}
}

// tryFollow reports whether call is a followable helper call: a bare
// identifier (respond(...)), a qualified package-level identifier
// (httputil.Success(...)), or a method on a type declared in the
// analyzed module (s.respond(...) — a server struct carrying response
// helpers is a common real-world shape; the receiver isn't a parameter,
// so bindParams binds the plain arguments positionally exactly as for a
// function). An *external* method like a framework context's c.JSON(...)
// resolves to a real object too, but declines at the funcIndex lookup —
// only the analyzed module's declarations are indexed — not because of
// any go/types gap. All of these resolve via funcIndex to a declaration
// with a body. On success,
// returns the callee's body statements and a new context: info switched
// to the callee's own package (astutil.FuncDeclInfo.Info, which may
// differ from ctx.info), subst bound from the call's arguments (see
// bindParams), depth incremented, and the callee added to visiting.
// Declines (ok=false, never an error) at maxFollowDepth, for a callee
// already in the current chain (a cycle), for a variadic callee, or an
// argument/parameter count mismatch.
func tryFollow(call *ast.CallExpr, ctx *evalCtx) (stmts []ast.Stmt, newCtx *evalCtx, ok bool) {
	if ctx.depth >= maxFollowDepth {
		return nil, nil, false
	}
	obj := followCallee(call, ctx)
	if obj == nil || ctx.visiting[obj] {
		return nil, nil, false
	}
	fd, found := ctx.funcIndex[obj]
	if !found || fd.Decl.Body == nil {
		return nil, nil, false
	}
	subst, ok := bindParams(fd.Decl, call.Args, fd.Info, ctx)
	if !ok {
		return nil, nil, false
	}
	visiting := make(map[types.Object]bool, len(ctx.visiting)+1)
	for v := range ctx.visiting {
		visiting[v] = true
	}
	visiting[obj] = true
	return fd.Decl.Body.List, &evalCtx{
		info:      fd.Info,
		subst:     subst,
		funcIndex: ctx.funcIndex,
		depth:     ctx.depth + 1,
		visiting:  visiting,
		dialect:   ctx.dialect,
		ambiguous: ctx.ambiguous,
	}, true
}

// followCallee resolves call.Fun to the types.Object it refers to: a
// bare identifier via its own Uses entry, or a selector via the
// selection's Sel — which covers BOTH qualified package-level
// identifiers (pkg.Func) and method selectors (recv.Method): go/types'
// recordSelection registers the selected method object in Uses too
// (GOROOT go/types/check.go, recordUse(x.Sel, obj)), it is NOT
// Selections-only. Whether a resolved method is actually followed is
// decided by tryFollow's funcIndex lookup, not here.
func followCallee(call *ast.CallExpr, ctx *evalCtx) types.Object {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return ctx.info.Uses[fn]
	case *ast.SelectorExpr:
		return ctx.info.Uses[fn.Sel]
	}
	return nil
}

// bindParams pairs decl's parameters positionally with call-site
// argument expressions args, resolving each parameter name to its
// *types.Var via declInfo — the *callee's own* Info, required because
// Defs/Uses are per-Info and the callee's parameter declarations only
// exist in its own package's type-checking pass; passing the wrong Info
// wouldn't error, it would silently bind nothing. Each argument is
// stored as a boundExpr paired with callerCtx (the context the call
// site's own expressions must be interpreted under, which is whatever
// context was active where this call was found — not declInfo). A
// single *ast.Field can hold multiple names sharing one type ("func f(a,
// b int)" is one Field with Names: [a, b], not two fields) — every name
// is bound to its own positional argument. Declines (ok=false) for a
// variadic parameter list or an argument/parameter count mismatch,
// rather than mis-binding.
func bindParams(decl *ast.FuncDecl, args []ast.Expr, declInfo *types.Info, callerCtx *evalCtx) (map[*types.Var]boundExpr, bool) {
	if decl.Type.Params == nil {
		return nil, len(args) == 0
	}
	var names []*ast.Ident
	for _, field := range decl.Type.Params.List {
		if _, variadic := field.Type.(*ast.Ellipsis); variadic {
			return nil, false
		}
		if len(field.Names) == 0 {
			// An unnamed parameter occupies an argument position but
			// can't be referenced inside the body, so it needs no binding.
			names = append(names, nil)
			continue
		}
		names = append(names, field.Names...)
	}
	if len(names) != len(args) {
		return nil, false
	}
	subst := make(map[*types.Var]boundExpr, len(names))
	for i, name := range names {
		if name == nil {
			continue
		}
		if v, ok := declInfo.Defs[name].(*types.Var); ok {
			subst[v] = boundExpr{expr: args[i], ctx: callerCtx}
		}
	}
	return subst, true
}

// responseCollector accumulates one Response per distinct status code
// found while walking a handler body.
type responseCollector struct {
	responses map[string]model.Response
}

// walk processes stmts in source order, threading the status code in
// effect (code) through sibling statements. Recursing into a nested block
// (if/else branch, for/switch body) passes a copy of code as that block's
// starting point; changes made inside never propagate back to the caller,
// which is what keeps sibling branches from contaminating each other.
func (rc *responseCollector) walk(stmts []ast.Stmt, code int, ctx *evalCtx, cmap ast.CommentMap) {
	for _, stmt := range stmts {
		code = rc.walkStmt(stmt, code, ctx, cmap)
	}
}

// walkStmt processes one statement and returns the status code in effect
// for the *next sibling* statement in the same block. A statement with a
// "gota:" comment declaring "x-gota-skip: true" attached to it (via cmap)
// is invisible to detection entirely: it doesn't record a response and
// doesn't update the code in effect.
//
// DeferStmt and GoStmt are skipped deliberately, not omissions: a
// deferred call runs at function exit, so pairing it with the code in
// effect at its *statement position* — the only model this walk has —
// would be wrong more often than right; and a response written from a
// spawned goroutine is a data race in the program under analysis, not
// a shape worth documenting.
func (rc *responseCollector) walkStmt(stmt ast.Stmt, code int, ctx *evalCtx, cmap ast.CommentMap) int {
	if isSkipped(stmt, cmap) {
		return code
	}
	switch s := stmt.(type) {
	case *ast.ExprStmt:
		if call, ok := s.X.(*ast.CallExpr); ok {
			code = rc.walkCall(call, code, ctx, cmap)
		}
	case *ast.AssignStmt:
		for _, rhs := range s.Rhs {
			if call, ok := rhs.(*ast.CallExpr); ok {
				code = rc.walkCall(call, code, ctx, cmap)
			}
		}
	case *ast.ReturnStmt:
		// return json.NewEncoder(w).Encode(v) — the whole response
		// written in return position, ubiquitous inside error-returning
		// helpers (and the entire idiom of frameworks like Echo). Any
		// ambient change is discarded: nothing runs after a return, so
		// there is no sibling for it to apply to.
		for _, result := range s.Results {
			if call, ok := result.(*ast.CallExpr); ok {
				rc.walkCall(call, code, ctx, cmap)
			}
		}
	case *ast.BlockStmt:
		rc.walk(s.List, code, ctx, cmap)
	case *ast.IfStmt:
		// The Init statement (if err := respond(w, v); err != nil)
		// runs unconditionally before the condition, exactly like a
		// statement written on the line above — so its effect on the
		// code in effect flows into the branches *and* to subsequent
		// siblings.
		if s.Init != nil {
			code = rc.walkStmt(s.Init, code, ctx, cmap)
		}
		rc.walk(s.Body.List, code, ctx, cmap)
		if s.Else != nil {
			rc.walkStmt(s.Else, code, ctx, cmap)
		}
	case *ast.ForStmt:
		if s.Body != nil {
			rc.walk(s.Body.List, code, ctx, cmap)
		}
	case *ast.RangeStmt:
		if s.Body != nil {
			rc.walk(s.Body.List, code, ctx, cmap)
		}
	case *ast.SwitchStmt:
		if s.Init != nil {
			code = rc.walkStmt(s.Init, code, ctx, cmap)
		}
		for _, clause := range s.Body.List {
			if cc, ok := clause.(*ast.CaseClause); ok {
				rc.walk(cc.Body, code, ctx, cmap)
			}
		}
	case *ast.TypeSwitchStmt:
		if s.Init != nil {
			code = rc.walkStmt(s.Init, code, ctx, cmap)
		}
		for _, clause := range s.Body.List {
			if cc, ok := clause.(*ast.CaseClause); ok {
				rc.walk(cc.Body, code, ctx, cmap)
			}
		}
	}
	return code
}

// isSkipped reports whether stmt has a "gota:" comment attached (via
// cmap) declaring "x-gota-skip: true".
func isSkipped(stmt ast.Stmt, cmap ast.CommentMap) bool {
	if cmap == nil {
		return false
	}
	for _, group := range cmap[stmt] {
		op, found, err := extractor.Extract(group)
		if err != nil || !found {
			continue
		}
		if op.Skip {
			return true
		}
	}
	return false
}

// walkCall inspects a single call expression, consulting the dialect
// first: a recognized call applies its responseEffect — recording a
// response at its explicit code or the code currently in effect, and/or
// changing the code in effect for subsequent statements in the same
// block — and is never also followed. A call the dialect doesn't
// recognize, but resolving to a followable helper (see tryFollow), is
// walked into with the same code in effect — anything it records lands
// in this same collector, and "code in effect" for what follows the
// call site is unaffected by what happened inside (matching real
// net/http semantics: a helper's own WriteHeader is a terminal write).
//
// A recorded effect whose value can't be converted to a schema records
// nothing when the code was ambient (an Encode at an unknowable-schema
// value is no evidence a response happened at a knowable code — see
// RespondViaHelperNilData's rationale in the fixture), but a *bare*
// response when the code was explicit: the status is statically certain
// even when the payload isn't (a future c.JSON(404, someInterfaceVar)
// still documents the 404). For the net/http dialect the distinction is
// currently unreachable — its only explicit-code effect (http.Error) is
// always schema-less by design — so this is vocabulary for future
// dialects, not a behavior change.
func (rc *responseCollector) walkCall(call *ast.CallExpr, code int, ctx *evalCtx, cmap ast.CommentMap) int {
	if eff, ok := ctx.dialect.response(call, ctx); ok {
		if eff.record {
			effCode := code
			if eff.code != 0 {
				effCode = eff.code
			}
			if eff.value == nil {
				rc.record(effCode, nil)
			} else if schema, ok := valueSchema(eff.value, eff.valueCtx); ok {
				rc.record(effCode, schema)
			} else if eff.code != 0 {
				rc.record(eff.code, nil)
			}
		}
		if eff.ambient > 0 {
			return eff.ambient
		}
		return code
	}
	if stmts, followCtx, ok := tryFollow(call, ctx); ok {
		rc.walk(stmts, code, followCtx, cmap)
	}
	return code
}

// record stores (or overwrites, if code was already seen — last one in
// source order wins) the response for a status code.
func (rc *responseCollector) record(code int, schema *model.Schema) {
	desc := http.StatusText(code)
	if desc == "" {
		desc = "Response"
	}
	resp := model.Response{Description: desc}
	if schema != nil {
		resp.Content = map[string]model.MediaType{"application/json": {Schema: schema}}
	}
	rc.responses[strconv.Itoa(code)] = resp
}

// firstBodySchema returns the schema for the first call in body that
// the dialect recognizes as a decode and whose target converts via
// valueSchema. A call not directly recognized, but resolving to a
// followable helper (see tryFollow), is searched into before moving on
// — the first match found there (if any) wins, same as anywhere else
// in source order.
func firstBodySchema(body ast.Node, ctx *evalCtx) (*model.Schema, bool) {
	var result *model.Schema
	ast.Inspect(body, func(n ast.Node) bool {
		if result != nil {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if expr, exprCtx, ok := ctx.dialect.decodeTarget(call, ctx); ok {
			if schema, ok := valueSchema(expr, exprCtx); ok {
				result = schema
				return false
			}
		}
		if stmts, followCtx, ok := tryFollow(call, ctx); ok {
			for _, stmt := range stmts {
				if schema, found := firstBodySchema(stmt, followCtx); found {
					result = schema
					return false
				}
			}
		}
		return true
	})
	return result, result != nil
}

// isPackageIdent reports whether x is a reference to the package at path
// itself (as in "json.Unmarshal" or "http.Error").
func isPackageIdent(x ast.Expr, info *types.Info, path string) bool {
	ident, ok := x.(*ast.Ident)
	if !ok {
		return false
	}
	pn, ok := info.Uses[ident].(*types.PkgName)
	return ok && pn.Imported().Path() == path
}

// addressedType returns v, the addressed expression in a "&v"
// expression, alongside the context it must be interpreted under.
// Resolves e through ctx first — a followed callee's own decode-target
// parameter is a bare identifier (e.g. "v" in "func decodeJSON(r
// *http.Request, v any) error"), not literally "&v"; the call site's
// actual argument ("&someVar") is what needs to surface before the "is
// this &x" check below, or it fails closed every time — and resolves
// the addressed operand itself a second time, in case it's *also* a
// bound parameter reference from a still-earlier frame.
func addressedType(e ast.Expr, ctx *evalCtx) (ast.Expr, *evalCtx, bool) {
	e, ctx = resolveExpr(e, ctx)
	unary, ok := e.(*ast.UnaryExpr)
	if !ok || unary.Op != token.AND {
		return nil, nil, false
	}
	x, xCtx := resolveExpr(unary.X, ctx)
	return x, xCtx, true
}

// constIntArg evaluates e (resolved through ctx first, same reasoning as
// addressedType) as a compile-time integer constant (a literal like 404
// or a named constant like http.StatusNotFound), returning false for
// anything computed at runtime.
func constIntArg(e ast.Expr, ctx *evalCtx) (int, bool) {
	e, ctx = resolveExpr(e, ctx)
	tv, ok := ctx.info.Types[e]
	if !ok || tv.Value == nil {
		return 0, false
	}
	n, ok := constant.Int64Val(tv.Value)
	if !ok {
		return 0, false
	}
	return int(n), true
}

// shallowRefSchema converts t into a Schema that references named struct
// types by $ref (and arrays/maps of them), without expanding struct
// fields — the separate ResolveSchemaRefs pass does that expansion once,
// however the $ref was produced (a "gota:" comment or this detector) —
// plus an inline schema for basic types (string, int, ...) and
// string-keyed maps. A *named* type over a map or basic (e.g. "type
// StatusMap map[string]any") isn't unwrapped and still reports false —
// only the outer type is matched, not Underlying() — a known,
// deliberate limit, not attempted here. An interface, a non-string-keyed
// map's key, or anything else with no JSON Schema equivalent also
// reports false, except when it's a map's *element* type: see the Map
// case below for why that specific failure degrades instead of
// propagating.
func shallowRefSchema(t types.Type, ambiguous, roots map[string]bool) (*model.Schema, bool) {
	switch tt := t.(type) {
	case *types.Pointer:
		return shallowRefSchema(tt.Elem(), ambiguous, roots)
	case *types.Named:
		if _, isStruct := tt.Underlying().(*types.Struct); !isStruct {
			return nil, false
		}
		// A named struct declared inside a function (or otherwise not at its
		// package's top level) can't be a shared component — ResolveSchemaRefs
		// only looks up package-scope types by name, and a bare name would
		// collide with an unrelated same-named type — so INLINE its object
		// schema (fields expanded, named field types as their own $refs)
		// rather than emitting an unresolvable $ref or a bare {type: object}.
		// A throwaway registry reuses the struct/field logic; only the
		// returned inline object is kept, and its nested $refs are resolved
		// by ResolveSchemaRefs the same as any other. A package-level type
		// (including an instantiated generic, whose Obj() is its package-level
		// origin) still gets a $ref to a shared component.
		if obj := tt.Obj(); obj.Pkg() == nil || obj.Pkg().Scope().Lookup(obj.Name()) != obj {
			reg := &registry{schemas: map[string]*model.Schema{}, ambiguous: ambiguous, roots: roots}
			return reg.schemaForType(tt.Underlying()), true
		}
		return &model.Schema{Ref: schemaRefPrefix + componentName(tt, ambiguous, roots)}, true
	case *types.Slice:
		item, ok := shallowRefSchema(tt.Elem(), ambiguous, roots)
		if !ok {
			return nil, false
		}
		return &model.Schema{Type: "array", Items: item}, true
	case *types.Array:
		item, ok := shallowRefSchema(tt.Elem(), ambiguous, roots)
		if !ok {
			return nil, false
		}
		return &model.Schema{Type: "array", Items: item}, true
	case *types.Map:
		if basic, ok := tt.Key().Underlying().(*types.Basic); !ok || basic.Kind() != types.String {
			return &model.Schema{Type: "object"}, true // non-string key: degrade, don't decline
		}
		// A map's element type failing to convert (e.g. map[string]any's
		// "any" element — exactly the common real-world envelope shape)
		// must degrade AdditionalProperties, not decline the whole map:
		// unlike Slice/Array, schema.go's own registry-based Map handling
		// (schemaForType) never fails here either, so propagating this
		// one failure would make gota worse at exactly the case that
		// motivated adding Map support in the first place.
		elem, _ := shallowRefSchema(tt.Elem(), ambiguous, roots)
		return &model.Schema{Type: "object", AdditionalProperties: elem}, true
	case *types.Basic:
		if tt.Kind() == types.UntypedNil || tt.Kind() == types.Invalid {
			return nil, false // e.g. a literal "nil" passed as the encoded value
		}
		return schemaForBasic(tt), true
	}
	return nil, false
}

// valueSchema converts e into a Schema, resolving it through ctx first —
// the sole resolution point every path into this function relies on
// (see encodeCallArg's doc comment). A map composite literal with every
// key a compile-time string constant (e.g. map[string]any{"status": "ok",
// "data": x} — a common way to wrap a real payload in an ad-hoc envelope
// with no named struct at all) gets an inline "object" schema built from
// its actual keys, via mapLiteralSchema; anything else — including a map
// literal with a dynamic key, which mapLiteralSchema declines — falls
// through to shallowRefSchema's type-based conversion.
func valueSchema(e ast.Expr, ctx *evalCtx) (*model.Schema, bool) {
	e, ctx = resolveExpr(e, ctx)
	if lit, ok := e.(*ast.CompositeLit); ok {
		if schema, ok := mapLiteralSchema(lit, ctx); ok {
			return schema, true
		}
	}
	t := ctx.info.TypeOf(e)
	if t == nil {
		return nil, false
	}
	return shallowRefSchema(t, ctx.ambiguous, ctx.roots)
}

// mapLiteralSchema converts lit, a map composite literal, into an inline
// "object" schema built from its actual key/value pairs — declines
// (ok=false) unless every key is a compile-time string constant and
// every value itself converts via valueSchema (recursively, so a nested
// map literal value, or a value that's itself a bound parameter
// reference resolving arbitrarily far up the follow chain, both work for
// free); a single unresolvable key or value declines the whole literal
// rather than silently dropping just that one entry, matching the
// all-or-nothing posture the indirection-following and method-dispatch-
// recognition features elsewhere in gota already established. Every
// listed key becomes required: a map literal's keys are unconditionally
// present, unlike a struct field that can be behind "omitempty".
func mapLiteralSchema(lit *ast.CompositeLit, ctx *evalCtx) (*model.Schema, bool) {
	// Underlying() so a NAMED map type — the common gin.H{...} envelope
	// ("type H map[string]any") — is recognized too, not only an
	// unnamed map[string]any literal; identity for the unnamed case.
	// Nil-guarded: nil.Underlying() panics (unlike a nil type assertion).
	t := ctx.info.TypeOf(lit)
	if t == nil {
		return nil, false
	}
	mapType, ok := t.Underlying().(*types.Map)
	if !ok {
		return nil, false
	}
	if basic, ok := mapType.Key().Underlying().(*types.Basic); !ok || basic.Kind() != types.String {
		return nil, false
	}
	if len(lit.Elts) == 0 {
		return nil, false
	}

	props := make(map[string]*model.Schema, len(lit.Elts))
	required := make([]string, 0, len(lit.Elts))
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			return nil, false
		}
		key, ok := constStringArg(kv.Key, ctx.info)
		if !ok {
			return nil, false
		}
		schema, ok := valueSchema(kv.Value, ctx)
		if !ok {
			return nil, false
		}
		props[key] = schema
		required = append(required, key)
	}
	return &model.Schema{Type: "object", Properties: props, Required: required}, true
}

// constStringArg evaluates e as a compile-time string constant (a
// literal like "status" or a named constant), the string-typed sibling
// of constIntArg above.
func constStringArg(e ast.Expr, info *types.Info) (string, bool) {
	tv, ok := info.Types[e]
	if !ok || tv.Value == nil || tv.Value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(tv.Value), true
}
