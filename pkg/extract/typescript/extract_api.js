#!/usr/bin/env node
/**
 * extract_api.js — extract the public API from a TypeScript source file.
 *
 * Usage: node extract_api.js <source.ts>
 *
 * Outputs JSON to stdout with the structure:
 * {
 *   "name": "module_name",
 *   "structs": { "ClassName": { "fields": {...}, "methods": {...} } },
 *   "interfaces": { "InterfaceName": { "methods": {...} } },
 *   "functions": { "funcName": { "params": [...], "returns": ["T"] } },
 *   "typedefs": { "TypeAlias": "underlying_type" }
 * }
 *
 * Uses the TypeScript compiler API — requires `typescript` to be importable.
 * Falls back to: global tsc, npx typescript, or bundled.
 */

const fs = require("fs");
const path = require("path");

let ts;
try {
  ts = require("typescript");
} catch {
  // Try common global paths
  const candidates = [
    path.join(process.env.HOME || "", "node_modules/typescript"),
    "/opt/homebrew/lib/node_modules/typescript",
    "/usr/local/lib/node_modules/typescript",
  ];
  for (const c of candidates) {
    try {
      ts = require(c);
      break;
    } catch {}
  }
  if (!ts) {
    process.stderr.write(
      "error: cannot find typescript module. Install with: npm install -g typescript\n"
    );
    process.exit(1);
  }
}

function typeToString(checker, type) {
  if (!type) return "any";
  return checker.typeToString(
    type,
    undefined,
    ts.TypeFormatFlags.NoTruncation
  );
}

function getSourceFile(node) {
  while (node && !ts.isSourceFile(node)) node = node.parent;
  return node;
}

function nodeLocation(node) {
  const sf = getSourceFile(node);
  if (!sf) return { file: "", line: 0 };
  const { line } = sf.getLineAndCharacterOfPosition(node.getStart());
  return { file: path.basename(sf.fileName), line: line + 1 };
}

function getJSDoc(node) {
  // Get leading comment text (JSDoc or //)
  const sf = getSourceFile(node);
  if (!sf) return "";
  const fullText = sf.getFullText();
  const ranges = ts.getLeadingCommentRanges(fullText, node.getFullStart());
  if (!ranges || ranges.length === 0) return "";
  // Use the last comment block before the node
  const last = ranges[ranges.length - 1];
  let text = fullText.substring(last.pos, last.end);
  // Clean JSDoc: strip /** */ and leading *
  if (text.startsWith("/**")) {
    text = text.replace(/^\/\*\*\s*/, "").replace(/\s*\*\/\s*$/, "");
    text = text.replace(/^\s*\* ?/gm, "");
  } else if (text.startsWith("//")) {
    text = text.replace(/^\/\/\s?/gm, "");
  }
  return text.trim();
}

function nodeTypeString(checker, node) {
  if (node.type) {
    return node.type.getText();
  }
  try {
    const t = checker.getTypeAtLocation(node);
    return typeToString(checker, t);
  } catch {
    return "any";
  }
}

function paramInfo(checker, param) {
  const name = param.name.getText();
  let type = "any";
  if (param.type) {
    type = param.type.getText();
  } else {
    try {
      type = typeToString(checker, checker.getTypeAtLocation(param));
    } catch {}
  }
  if (param.questionToken) {
    type = type + " | undefined";
  }
  return { name, type };
}

function funcInfo(checker, node) {
  const params = [];
  for (const p of node.parameters) {
    params.push(paramInfo(checker, p));
  }
  let returns = ["void"];
  if (node.type) {
    returns = [node.type.getText()];
  } else {
    try {
      const sig = checker.getSignatureFromDeclaration(node);
      if (sig) {
        const retType = checker.getReturnTypeOfSignature(sig);
        returns = [typeToString(checker, retType)];
      }
    } catch {}
  }
  return { params, returns };
}

function isExported(node) {
  // Check for export modifier
  if (
    node.modifiers &&
    node.modifiers.some((m) => m.kind === ts.SyntaxKind.ExportKeyword)
  ) {
    return true;
  }
  // Check for `export default`
  if (node.kind === ts.SyntaxKind.ExportAssignment) return true;
  return false;
}

function isPublicMember(node) {
  // Class members: skip private/protected
  if (
    node.modifiers &&
    node.modifiers.some(
      (m) =>
        m.kind === ts.SyntaxKind.PrivateKeyword ||
        m.kind === ts.SyntaxKind.ProtectedKeyword
    )
  ) {
    return false;
  }
  // Skip names starting with _
  if (node.name && node.name.getText().startsWith("_")) return false;
  return true;
}

function extractClass(checker, node) {
  const fields = {};
  const methods = {};
  const loc = nodeLocation(node);

  for (const member of node.members) {
    if (!isPublicMember(member)) continue;

    if (ts.isPropertyDeclaration(member) && member.name) {
      const name = member.name.getText();
      fields[name] = member.type ? member.type.getText() : "any";
    } else if (ts.isMethodDeclaration(member) && member.name) {
      const name = member.name.getText();
      const fi = funcInfo(checker, member);
      fi.doc = getJSDoc(member);
      const mloc = nodeLocation(member);
      fi.file = mloc.file;
      fi.line = mloc.line;
      methods[name] = fi;
    } else if (ts.isConstructorDeclaration(member)) {
      // Extract parameter properties (public/readonly params in constructor)
      for (const p of member.parameters) {
        if (
          p.modifiers &&
          p.modifiers.some(
            (m) =>
              m.kind === ts.SyntaxKind.PublicKeyword ||
              m.kind === ts.SyntaxKind.ReadonlyKeyword
          )
        ) {
          const name = p.name.getText();
          fields[name] = p.type ? p.type.getText() : "any";
        }
      }
      // Constructor itself as a method
      if (member.parameters.length > 0) {
        methods["constructor"] = funcInfo(checker, member);
      }
    } else if (ts.isGetAccessorDeclaration(member) && member.name) {
      // Getters appear as fields
      const name = member.name.getText();
      fields[name] = member.type ? member.type.getText() : "any";
    }
  }

  return { fields, methods, doc: getJSDoc(node), file: loc.file, line: loc.line };

}

function extractInterface(checker, node) {
  const methods = {};
  const fields = {};
  const loc = nodeLocation(node);

  for (const member of node.members) {
    if (ts.isMethodSignature(member) && member.name) {
      const fi = funcInfo(checker, member);
      fi.doc = getJSDoc(member);
      const mloc = nodeLocation(member);
      fi.file = mloc.file;
      fi.line = mloc.line;
      methods[member.name.getText()] = fi;
    } else if (ts.isPropertySignature(member) && member.name) {
      const name = member.name.getText();
      if (member.type && ts.isFunctionTypeNode(member.type)) {
        methods[name] = funcInfo(checker, member.type);
      } else {
        fields[name] = member.type ? member.type.getText() : "any";
      }
    } else if (ts.isCallSignatureDeclaration(member)) {
      methods["__call"] = funcInfo(checker, member);
    }
  }

  return { fields, methods, doc: getJSDoc(node), file: loc.file, line: loc.line };
}

function extract(filePath) {
  const fileName = path.basename(filePath, path.extname(filePath));
  const result = {
    name: fileName,
    structs: {},
    interfaces: {},
    functions: {},
    typedefs: {},
  };

  const program = ts.createProgram([filePath], {
    target: ts.ScriptTarget.ESNext,
    module: ts.ModuleKind.ESNext,
    strict: false,
    skipLibCheck: true,
    noResolve: true,
  });

  const sourceFile = program.getSourceFile(filePath);
  if (!sourceFile) {
    process.stderr.write(`error: cannot parse ${filePath}\n`);
    process.exit(1);
  }

  const checker = program.getTypeChecker();

  // Determine if this is a module file (has any exports) or a script
  let hasExports = false;
  ts.forEachChild(sourceFile, (node) => {
    if (isExported(node)) hasExports = true;
  });

  ts.forEachChild(sourceFile, (node) => {
    // In module files, only extract exported symbols.
    // In script files (no exports), extract all top-level public symbols.
    if (hasExports && !isExported(node)) return;

    if (ts.isClassDeclaration(node) && node.name) {
      result.structs[node.name.getText()] = extractClass(checker, node);
    } else if (ts.isInterfaceDeclaration(node) && node.name) {
      result.interfaces[node.name.getText()] = extractInterface(checker, node);
    } else if (ts.isFunctionDeclaration(node) && node.name) {
      const fi = funcInfo(checker, node);
      fi.doc = getJSDoc(node);
      const loc = nodeLocation(node);
      fi.file = loc.file;
      fi.line = loc.line;
      result.functions[node.name.getText()] = fi;
    } else if (ts.isTypeAliasDeclaration(node) && node.name) {
      const loc = nodeLocation(node);
      result.typedefs[node.name.getText()] = {
        underlying: node.type.getText(),
        doc: getJSDoc(node),
        file: loc.file,
        line: loc.line,
      };
    } else if (ts.isVariableStatement(node)) {
      const vloc = nodeLocation(node);
      const vdoc = getJSDoc(node);
      for (const decl of node.declarationList.declarations) {
        if (ts.isIdentifier(decl.name)) {
          const name = decl.name.getText();
          if (name.startsWith("_")) continue;
          if (decl.type) {
            const typeText = decl.type.getText();
            if (ts.isFunctionTypeNode(decl.type)) {
              const fi = funcInfo(checker, decl.type);
              fi.doc = vdoc;
              fi.file = vloc.file;
              fi.line = vloc.line;
              result.functions[name] = fi;
            } else {
              result.typedefs[name] = { underlying: typeText, doc: vdoc, file: vloc.file, line: vloc.line };
            }
          } else if (
            decl.initializer &&
            (ts.isArrowFunction(decl.initializer) ||
              ts.isFunctionExpression(decl.initializer))
          ) {
            const fi = funcInfo(checker, decl.initializer);
            fi.doc = vdoc;
            fi.file = vloc.file;
            fi.line = vloc.line;
            result.functions[name] = fi;
          } else {
            try {
              const t = checker.getTypeAtLocation(decl);
              result.typedefs[name] = { underlying: typeToString(checker, t), doc: vdoc, file: vloc.file, line: vloc.line };
            } catch {
              result.typedefs[name] = { underlying: "any", doc: vdoc, file: vloc.file, line: vloc.line };
            }
          }
        }
      }
    } else if (ts.isEnumDeclaration(node) && node.name) {
      const loc = nodeLocation(node);
      result.typedefs[node.name.getText()] = {
        underlying: "enum",
        doc: getJSDoc(node),
        file: loc.file,
        line: loc.line,
      };
    }
  });

  return result;
}

// --- main ---
if (process.argv.length < 3) {
  process.stderr.write("usage: node extract_api.js <source.ts>\n");
  process.exit(1);
}

const filePath = path.resolve(process.argv[2]);
if (!fs.existsSync(filePath)) {
  process.stderr.write(`error: file not found: ${filePath}\n`);
  process.exit(1);
}

const data = extract(filePath);
process.stdout.write(JSON.stringify(data, null, 2) + "\n");
