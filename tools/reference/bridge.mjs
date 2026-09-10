// Upstream oracle only. Go owns inputs, files, compression, checksums, and orchestration.
// Keep each suite in a separate process: Shiki has mutable language/theme caches.
import { readFileSync } from 'node:fs'
import { bundledLanguagesInfo, bundledThemesInfo, createHighlighter, hastToHtml, normalizeTheme } from 'shiki'

const input = JSON.parse(readFileSync(0, 'utf8'))
const operation = process.argv[2]
let output
if (operation === 'assets') {
  const languages = {}, themes = {}, properties = {}
  for (const info of bundledLanguagesInfo)
    for (const lang of (await info.import()).default) languages[lang.name] = JSON.stringify(lang)
  for (const info of bundledThemesInfo) themes[info.id] = JSON.stringify((await info.import()).default)
  const { html, svg } = await import('property-information')
  for (const [name, schema] of Object.entries({ html, svg })) {
    properties[name] = Object.fromEntries(Object.entries(schema.normal).map(([key, prop]) => {
      const info = schema.property[prop]
      return [key, { attribute: info.attribute, boolean: info.boolean || undefined, overloadedBoolean: info.overloadedBoolean || undefined, commaSeparated: info.commaSeparated || undefined }]
    }))
  }
  output = {
    catalog: {version: input.version, languages: bundledLanguagesInfo.map(({import: _, ...info}) => info), themes: bundledThemesInfo.map(({import: _, ...info}) => info)},
    languages, themes, properties,
  }
} else if (operation === 'suite') {
  if (input.kind === 'hast') {
    output = []
    for (const [i, node] of input.nodes.entries()) for (const options of input.options)
      output.push({name: `hast-${i}-${output.length}`, node, options, html: hastToHtml(node, options)})
  } else {
    const setup = {...input.highlighter}
    if (setup.langs === '*') setup.langs = bundledLanguagesInfo.map(info => info.id)
    const h = await createHighlighter(setup)
    try {
      output = []
      if (input.kind === 'stream') {
        const { ShikiStreamTokenizer } = await import('@shikijs/stream')
        for (const {name, options, chunks} of input.cases) {
          const tokenizer = new ShikiStreamTokenizer({...options, highlighter: h})
          const results = []
          for (const chunk of chunks) results.push(structuredClone(await tokenizer.enqueue(chunk)))
          output.push({name, options, chunks, results, closed: tokenizer.close().stable})
        }
      } else if (input.kind === 'transformers') {
        const transformers = await import('@shikijs/transformers')
        const { transformerColorizedBrackets } = await import('@shikijs/colorized-brackets')
        for (const {name, transformer, config, code, options} of input.cases) {
          const factory = transformer === 'colorizedBrackets' ? transformerColorizedBrackets : transformers['transformer' + transformer[0].toUpperCase() + transformer.slice(1)]
          const tr = factory(config)
          output.push({name, transformer, config, code, options, html: h.codeToHtml(code, {...options, transformers: [tr]}), css: tr.getCSS?.()})
        }
      } else if (input.kind === 'highlight' || input.kind === 'advanced') {
        const add = item => {
          const {name, code, options, prefix} = item
          const actual = prefix === undefined ? options : {...options, grammarState: h.getLastGrammarState(prefix, options)}
          const result = {name, code, options, ...(prefix === undefined ? {} : {prefix}), tokens: h.codeToTokens(code, actual), html: h.codeToHtml(code, actual)}
          if (item.hast) result.hast = h.codeToHast(code, actual)
          if (input.kind === 'advanced' && options.themes) result.variants = h.codeToTokensWithThemes(code, actual)
          output.push(result)
        }
        for (const item of input.cases || []) add(item)
        if (input.allLanguages) for (const {id} of bundledLanguagesInfo)
          add({name: id, code: input.allLanguages.code, options: {lang: id, ...input.allLanguages.options}})
        if (input.allThemes) for (const {id} of bundledThemesInfo) {
          await h.loadTheme(id)
          add({name: `theme-${id}`, code: input.allThemes.code, options: {...input.allThemes.options, theme: id}})
        }
        if (input.kind === 'advanced') {
          process.env.FORCE_COLOR = '3'
          delete process.env.NO_COLOR
          const { codeToANSI } = await import('@shikijs/cli')
          const ansi = []
          for (const {name, code, options} of input.ansi)
            ansi.push({name, code, options, ansi: await codeToANSI(code, options.lang, options.theme)})
          output = {grammars: input.grammars, themes: input.themes, normalized: input.themes.map(normalizeTheme), cases: output, ansi}
        }
      } else {
        throw new Error(`Unknown suite kind: ${input.kind}`)
      }
    } finally { h.dispose() }
  }
} else {
  throw new Error(`Unknown bridge operation: ${operation}`)
}
process.stdout.write(JSON.stringify(output))
