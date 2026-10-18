// @ts-check
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { GhError, missingScopeNotice } from '../lib/gh.mjs';

test('a refused billing call exposes the scope gh says it needs', () => {
  // stderr captured from `gh api /users/guyravid/settings/billing/usage` without the user scope (2026-10-17).
  const stderr =
    'gh: Not Found (HTTP 404)\ngh: This API operation needs the "user" scope. To request it, run:  gh auth refresh -h github.com -s user\n';
  const error = new GhError('failed', stderr);
  assert.equal(error.missingScope, 'user');
  assert.equal(error.isNotFound, true);
  assert.equal(new GhError('failed', 'gh: Not Found (HTTP 404)').missingScope, null);
});

test('missing-scope notice gives the exact refresh command', () => {
  const notice = missingScopeNotice('user', 'Billing API unavailable.');
  assert.match(notice, /missing the "user" scope/);
  assert.match(notice, /gh auth refresh -h github\.com -s user/);
});
