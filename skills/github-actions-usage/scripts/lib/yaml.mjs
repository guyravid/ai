// @ts-check
// Minimal YAML reader for GitHub workflow files: block mappings and sequences, flow lists/maps,
// quoted and plain scalars, block scalars (| and >). Anchors, aliases, tags and multi-document
// streams are not supported; workflows rarely use them. Records the source line of every mapping key
// so findings can cite file:line.

/** @type {WeakMap<object, Record<string, number>>} */
const keyLines = new WeakMap();

/**
 * 1-based source line of `key` inside a parsed mapping, or undefined.
 * @param {unknown} node
 * @param {string} key
 * @returns {number | undefined}
 */
export function lineOf(node, key) {
  if (!node || typeof node !== 'object') return undefined;
  return keyLines.get(node)?.[key];
}

/**
 * @typedef {{ indent: number, text: string, line: number }} Line
 */

/**
 * Strip a trailing comment: a '#' at line start or after whitespace, outside quotes.
 * @param {string} text
 * @returns {string}
 */
function stripComment(text) {
  let quote = '';
  for (let index = 0; index < text.length; index++) {
    const char = text[index];
    if (quote) {
      if (char === quote) quote = '';
      continue;
    }
    if (char === '"' || char === "'") quote = char;
    else if (char === '#' && (index === 0 || /\s/.test(text[index - 1]))) return text.slice(0, index);
  }
  return text;
}

/**
 * Split on commas not nested inside brackets, braces or quotes.
 * @param {string} body
 * @returns {string[]}
 */
function splitTopLevel(body) {
  const parts = [];
  let depth = 0;
  let quote = '';
  let current = '';
  for (const char of body) {
    if (quote) {
      if (char === quote) quote = '';
    } else if (char === '"' || char === "'") quote = char;
    else if (char === '[' || char === '{') depth++;
    else if (char === ']' || char === '}') depth--;
    else if (char === ',' && depth === 0) {
      parts.push(current);
      current = '';
      continue;
    }
    current += char;
  }
  if (current.trim() !== '') parts.push(current);
  return parts.map((part) => part.trim());
}

/**
 * Index of the ':' that separates a mapping key from its value, or -1.
 * @param {string} text
 * @returns {number}
 */
function keySeparator(text) {
  let quote = '';
  let depth = 0;
  for (let index = 0; index < text.length; index++) {
    const char = text[index];
    if (quote) {
      if (char === quote) quote = '';
      continue;
    }
    if (char === '"' || char === "'") quote = char;
    else if (char === '[' || char === '{') depth++;
    else if (char === ']' || char === '}') depth--;
    else if (char === ':' && depth === 0 && (index === text.length - 1 || text[index + 1] === ' ')) return index;
  }
  return -1;
}

/**
 * @param {string} raw
 * @returns {unknown}
 */
function parseScalar(raw) {
  const text = raw.trim();
  if (text.startsWith('[') && text.endsWith(']')) {
    return splitTopLevel(text.slice(1, -1)).map(parseScalar);
  }
  if (text.startsWith('{') && text.endsWith('}')) {
    /** @type {Record<string, unknown>} */
    const map = {};
    for (const part of splitTopLevel(text.slice(1, -1))) {
      const separator = keySeparator(part);
      if (separator === -1) continue;
      map[String(parseScalar(part.slice(0, separator)))] = parseScalar(part.slice(separator + 1));
    }
    return map;
  }
  if ((text.startsWith('"') && text.endsWith('"')) || (text.startsWith("'") && text.endsWith("'"))) {
    return text.slice(1, -1);
  }
  if (text === '' || text === '~' || text === 'null') return null;
  if (text === 'true') return true;
  if (text === 'false') return false;
  if (/^-?\d+(\.\d+)?$/.test(text)) return Number(text);
  return text;
}

/**
 * @param {string} source
 * @returns {{ lines: Line[], raw: string[] }}
 */
function tokenize(source) {
  const raw = source.split(/\r?\n/);
  /** @type {Line[]} */
  const lines = [];
  for (let index = 0; index < raw.length; index++) {
    const text = stripComment(raw[index]).replace(/\s+$/, '');
    if (text.trim() === '' || text.trim() === '---') continue;
    lines.push({ indent: text.length - text.trimStart().length, text: text.trim(), line: index + 1 });
  }
  return { lines, raw };
}

class Parser {
  /**
   * @param {Line[]} lines
   * @param {string[]} raw
   */
  constructor(lines, raw) {
    this.lines = lines;
    this.raw = raw;
    this.position = 0;
  }

  /** @returns {Line | undefined} */
  peek() {
    return this.lines[this.position];
  }

  /**
   * @param {number} indent
   * @returns {unknown}
   */
  parseBlock(indent) {
    const first = this.peek();
    if (!first) return null;
    if (first.text === '-' || first.text.startsWith('- ')) return this.parseSequence(first.indent);
    return this.parseMapping(Math.max(indent, first.indent));
  }

  /**
   * @param {number} indent
   * @returns {Record<string, unknown>}
   */
  parseMapping(indent) {
    /** @type {Record<string, unknown>} */
    const map = {};
    /** @type {Record<string, number>} */
    const lines = {};
    keyLines.set(map, lines);
    for (let current = this.peek(); current && current.indent === indent; current = this.peek()) {
      if (current.text === '-' || current.text.startsWith('- ')) break;
      const separator = keySeparator(current.text);
      if (separator === -1) {
        this.position++;
        continue;
      }
      const key = String(parseScalar(current.text.slice(0, separator)));
      const rest = current.text.slice(separator + 1).trim();
      lines[key] = current.line;
      this.position++;
      map[key] = this.parseValue(rest, indent);
    }
    return map;
  }

  /**
   * @param {string} rest text after "key:" or "- "
   * @param {number} parentIndent
   * @returns {unknown}
   */
  parseValue(rest, parentIndent) {
    if (/^[|>][-+]?\d*$/.test(rest)) return this.parseBlockScalar(parentIndent);
    if (rest === '') {
      const next = this.peek();
      if (!next) return null;
      const isSequenceAtSameIndent = next.indent === parentIndent && (next.text === '-' || next.text.startsWith('- '));
      if (next.indent > parentIndent || isSequenceAtSameIndent) return this.parseBlock(next.indent);
      return null;
    }
    if ((rest.startsWith('[') || rest.startsWith('{')) && !isBalanced(rest)) {
      let joined = rest;
      while (this.peek() && !isBalanced(joined)) {
        joined += ' ' + /** @type {Line} */ (this.peek()).text;
        this.position++;
      }
      return parseScalar(joined);
    }
    return parseScalar(rest);
  }

  /**
   * @param {number} parentIndent
   * @returns {string}
   */
  parseBlockScalar(parentIndent) {
    const collected = [];
    for (let current = this.peek(); current && current.indent > parentIndent; current = this.peek()) {
      collected.push(this.raw[current.line - 1]);
      this.position++;
    }
    return collected.join('\n');
  }

  /**
   * @param {number} indent
   * @returns {unknown[]}
   */
  parseSequence(indent) {
    const items = [];
    for (let current = this.peek(); current && current.indent === indent; current = this.peek()) {
      if (!(current.text === '-' || current.text.startsWith('- '))) break;
      const rest = current.text === '-' ? '' : current.text.slice(2).trim();
      if (rest !== '' && keySeparator(rest) !== -1 && !rest.startsWith('[') && !rest.startsWith('{') && !/^["']/.test(rest)) {
        // "- key: value" starts a mapping whose keys sit at the item's content indent.
        const contentIndent = indent + (current.text.length - current.text.slice(1).trimStart().length);
        this.lines[this.position] = { indent: contentIndent, text: rest, line: current.line };
        items.push(this.parseMapping(contentIndent));
      } else {
        this.position++;
        items.push(this.parseValue(rest, indent + 1));
      }
    }
    return items;
  }
}

/**
 * @param {string} text
 * @returns {boolean}
 */
function isBalanced(text) {
  let depth = 0;
  for (const char of text) {
    if (char === '[' || char === '{') depth++;
    else if (char === ']' || char === '}') depth--;
  }
  return depth <= 0;
}

/**
 * @param {string} source
 * @returns {Record<string, unknown>}
 */
export function parseYaml(source) {
  const { lines, raw } = tokenize(source);
  const parser = new Parser(lines, raw);
  const result = parser.parseBlock(0);
  return result && typeof result === 'object' && !Array.isArray(result) ? /** @type {Record<string, unknown>} */ (result) : {};
}
