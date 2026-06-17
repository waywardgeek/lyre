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

  for (const member of node.members) {
    if (!isPublicMember(member)) continue;

    if (ts.isPropertyDeclaration(member) && member.name) {
      const name = member.name.getText();
      fields[name] = member.type ? member.type.getText() : "any";
    } else if (ts.isMethodDeclaration(member) && member.name) {
      const name = member.name.getText();
      methods[name] = funcInfo(checker, member);
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

  return { fields, methods };
}

function extractInterface(checker, node) {
  const methods = {};
  const fields = {};

  for (const member of node.members) {
    if (ts.isMethodSignature(member) && member.name) {
      methods[member.name.getText()] = funcInfo(checker, member);
    } else if (ts.isPropertySignature(member) && member.name) {
      const name = member.name.getText();
      // If the type is a function signature, treat as method
      if (member.type && ts.isFunctionTypeNode(member.type)) {
        methods[name] = funcInfo(checker, member.type);
      } else {
        fields[name] = member.type ? member.type.getText() : "any";
      }
    } else if (ts.isCallSignatureDeclaration(member)) {
      methods["__call"] = funcInfo(checker, member);
    }
  }

  return { fields, methods };
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
      result.functions[node.name.getText()] = funcInfo(checker, node);
    } else if (ts.isTypeAliasDeclaration(node) && node.name) {
      result.typedefs[node.name.getText()] = node.type.getText();
    } else if (ts.isVariableStatement(node)) {
      // export const foo: Type = ...
      for (const decl of node.declarationList.declarations) {
        if (ts.isIdentifier(decl.name)) {
          const name = decl.name.getText();
          if (name.startsWith("_")) continue;
          if (decl.type) {
            const typeText = decl.type.getText();
            // If it's a function type, register as function
            if (ts.isFunctionTypeNode(decl.type)) {
              result.functions[name] = funcInfo(checker, decl.type);
            } else {
              result.typedefs[name] = typeText;
            }
          } else if (
            decl.initializer &&
            (ts.isArrowFunction(decl.initializer) ||
              ts.isFunctionExpression(decl.initializer))
          ) {
            result.functions[name] = funcInfo(checker, decl.initializer);
          } else {
            try {
              const t = checker.getTypeAtLocation(decl);
              result.typedefs[name] = typeToString(checker, t);
            } catch {
              result.typedefs[name] = "any";
            }
          }
        }
      }
    } else if (ts.isEnumDeclaration(node) && node.name) {
      // Enums → typedefs
      result.typedefs[node.name.getText()] = "enum";
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
