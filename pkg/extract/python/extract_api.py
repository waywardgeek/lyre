"""
extract_api.py — extract the public API from a Python source or stub file.

Usage: python3 extract_api.py <source.py|stub.py.lyric>

Outputs a JSON object to stdout with the structure:
{
  "name": "package_name",
  "structs": {
    "ClassName": {
      "fields": {"field": "type"},
      "methods": {
        "method": {"params": [{"name": "p", "type": "T"}], "returns": ["T"]}
      }
    }
  },
  "interfaces": {
    "ProtocolName": {
      "methods": { ... }
    }
  },
  "functions": {
    "func_name": {"params": [...], "returns": ["T"]}
  },
  "typedefs": {
    "TypeAlias": "underlying_type"
  }
}

Works with Python 3.8+. Does not require any third-party packages.
"""

import ast
import json
import os
import sys


def unparse_annotation(node):
    """Convert an AST annotation node to a type string. Python 3.8 compatible."""
    if node is None:
        return "Any"
    if isinstance(node, ast.Name):
        return node.id
    if isinstance(node, ast.Attribute):
        return f"{unparse_annotation(node.value)}.{node.attr}"
    if isinstance(node, ast.Subscript):
        val = unparse_annotation(node.value)
        slc = node.slice
        # Python 3.8 wraps slice in ast.Index
        if isinstance(slc, ast.Index):
            slc = slc.value  # type: ignore[attr-defined]
        return f"{val}[{unparse_annotation(slc)}]"
    if isinstance(node, ast.Tuple):
        return ", ".join(unparse_annotation(e) for e in node.elts)
    if isinstance(node, ast.List):
        return "[" + ", ".join(unparse_annotation(e) for e in node.elts) + "]"
    if isinstance(node, ast.Constant):
        if isinstance(node.value, str):
            return node.value  # forward reference string
        return repr(node.value)
    if isinstance(node, ast.BinOp) and isinstance(node.op, ast.BitOr):
        # Python 3.10+ union: X | Y
        return f"{unparse_annotation(node.left)} | {unparse_annotation(node.right)}"
    if isinstance(node, ast.IfExp):
        # Shouldn't appear in annotations but handle gracefully
        return "Any"
    # Last resort: try ast.unparse (Python 3.9+)
    try:
        return ast.unparse(node)  # type: ignore[attr-defined]
    except AttributeError:
        pass
    return "Any"


def func_info(node):
    """Extract params (excluding self/cls) and returns from a FunctionDef node."""
    args = node.args
    params = []
    # Collect all args: posonlyargs + args + kwonlyargs (skip self/cls at pos 0)
    all_args = list(args.posonlyargs) + list(args.args)
    # Skip first arg if it's self or cls
    start = 0
    if all_args and all_args[0].arg in ("self", "cls"):
        start = 1
    for arg in all_args[start:]:
        params.append({
            "name": arg.arg,
            "type": unparse_annotation(arg.annotation) if arg.annotation else "Any",
        })
    # kwonlyargs
    for arg in args.kwonlyargs:
        params.append({
            "name": arg.arg,
            "type": unparse_annotation(arg.annotation) if arg.annotation else "Any",
        })
    # vararg (*args)
    if args.vararg:
        params.append({
            "name": "*" + args.vararg.arg,
            "type": unparse_annotation(args.vararg.annotation) if args.vararg.annotation else "Any",
        })
    # kwarg (**kwargs)
    if args.kwarg:
        params.append({
            "name": "**" + args.kwarg.arg,
            "type": unparse_annotation(args.kwarg.annotation) if args.kwarg.annotation else "Any",
        })

    returns = []
    if node.returns is not None:
        ret = unparse_annotation(node.returns)
        if ret and ret != "None":
            returns = [ret]

    return {"params": params, "returns": returns}


def get_docstring(node):
    """Extract the first-line docstring from a function/class node, or ''."""
    return ast.get_docstring(node) or ""


def node_loc(node, filename):
    """Return (basename, lineno) for a node."""
    return os.path.basename(filename), getattr(node, "lineno", 0)


def is_public(name):
    """True if name should be treated as a public export."""
    return not name.startswith("_")


def extract_from_source(source_text, filename):
    """Parse Python source or stub text and return the API dict."""
    try:
        tree = ast.parse(source_text, filename=filename)
    except SyntaxError as e:
        print(f"SyntaxError in {filename}: {e}", file=sys.stderr)
        return None

    # Detect package name from filename (strip extensions)
    base = os.path.basename(filename)
    for suffix in (".py.lyric", ".pyi", ".py"):
        if base.endswith(suffix):
            base = base[: -len(suffix)]
            break
    pkg_name = base

    # Check __all__
    all_names = None
    for node in ast.walk(tree):
        if isinstance(node, ast.Assign):
            for target in node.targets:
                if isinstance(target, ast.Name) and target.id == "__all__":
                    if isinstance(node.value, (ast.List, ast.Tuple)):
                        all_names = set()
                        for elt in node.value.elts:
                            if isinstance(elt, ast.Constant) and isinstance(elt.value, str):
                                all_names.add(elt.value)

    result = {
        "name": pkg_name,
        "structs": {},
        "interfaces": {},
        "functions": {},
        "typedefs": {},
    }

    for node in tree.body:
        # Module-level functions
        if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
            name = node.name
            if not is_public(name):
                continue
            if all_names is not None and name not in all_names:
                continue
            result["functions"][name] = func_info(node)
            fi = result["functions"][name]
            fi["doc"] = get_docstring(node)
            fi["file"], fi["line"] = node_loc(node, filename)

        # Class definitions
        elif isinstance(node, ast.ClassDef):
            class_name = node.name
            if not is_public(class_name):
                continue
            if all_names is not None and class_name not in all_names:
                continue

            # Detect if this is a Protocol (treat as interface)
            bases = [
                unparse_annotation(b) for b in node.bases
            ]
            is_protocol = "Protocol" in bases or "typing.Protocol" in bases

            fields = {}
            methods = {}

            for item in node.body:
                # Class-level annotated assignments: x: int = ...
                if isinstance(item, ast.AnnAssign):
                    if isinstance(item.target, ast.Name):
                        fname = item.target.id
                        if is_public(fname):
                            fields[fname] = unparse_annotation(item.annotation)

                # Method definitions
                elif isinstance(item, (ast.FunctionDef, ast.AsyncFunctionDef)):
                    mname = item.name
                    # Skip private methods (but keep __init__ for field extraction)
                    if mname == "__init__":
                        # Extract typed params from __init__ as fields
                        args = item.args
                        all_args = list(args.posonlyargs) + list(args.args)
                        for arg in all_args[1:]:  # skip self
                            if arg.annotation and is_public(arg.arg):
                                # Only add if not already found via annotation
                                if arg.arg not in fields:
                                    fields[arg.arg] = unparse_annotation(arg.annotation)
                        continue
                    if not is_public(mname):
                        continue
                    # Strip decorators for naming (keep the method)
                    methods[mname] = func_info(item)

            if is_protocol:
                result["interfaces"][class_name] = {"methods": methods, "doc": get_docstring(node), "file": os.path.basename(filename), "line": node.lineno}
            else:
                result["structs"][class_name] = {"fields": fields, "methods": methods, "doc": get_docstring(node), "file": os.path.basename(filename), "line": node.lineno}

        # Type aliases: MyType = int  or  MyType: TypeAlias = int
        elif isinstance(node, ast.Assign):
            for target in node.targets:
                if isinstance(target, ast.Name) and is_public(target.id):
                    if all_names is not None and target.id not in all_names:
                        continue
                    # Only capture simple type aliases (e.g. MyType = List[int])
                    if isinstance(node.value, (ast.Name, ast.Attribute, ast.Subscript, ast.BinOp)):
                        result["typedefs"][target.id] = {"underlying": unparse_annotation(node.value), "file": os.path.basename(filename), "line": node.lineno}

        elif isinstance(node, ast.AnnAssign):
            # TypeAlias annotation: MyType: TypeAlias = List[int]
            if isinstance(node.target, ast.Name) and is_public(node.target.id):
                if node.value is not None and isinstance(
                    node.annotation, ast.Name
                ) and node.annotation.id == "TypeAlias":
                    result["typedefs"][node.target.id] = {"underlying": unparse_annotation(node.value), "file": os.path.basename(filename), "line": node.lineno}

    return result


def main():
    if len(sys.argv) < 2:
        print("usage: extract_api.py <source.py|stub.py.lyric>", file=sys.stderr)
        sys.exit(1)

    path = sys.argv[1]
    try:
        with open(path, "r", encoding="utf-8") as f:
            source = f.read()
    except OSError as e:
        print(f"cannot read {path}: {e}", file=sys.stderr)
        sys.exit(1)

    # For .py.lyric files, strip #ldd: comment lines before parsing
    # (they're valid Python comments, so ast.parse handles them fine)
    data = extract_from_source(source, path)
    if data is None:
        sys.exit(1)

    print(json.dumps(data, indent=2))


if __name__ == "__main__":
    main()
