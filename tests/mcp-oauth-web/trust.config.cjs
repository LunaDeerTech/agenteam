const original = require('./playwright.config.cjs')
module.exports = { ...original, testMatch: 'trust.spec.cjs', timeout: 20000 }
