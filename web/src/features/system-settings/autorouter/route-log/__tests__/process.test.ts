/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { expect, test } from 'vitest'

import { routeProcessSteps } from '../process'
import type { RouteLogDetail, RouteTrace } from '../types'

const request = { human_turns: 1, text_runes: 12 }
const trust = { threshold: 0.5, profiles: ['code.build'] }
const ladder = [{ profile: 'code.build', outcome: 'first_available' }]
const effort = { difficulty: 2, base: 'medium', threshold: 0.5 }
const continuation = {
  previous: 'coder',
  task_change: 0.9,
  threshold: 0.5,
  continued: false,
}

function detailOf(trace: Partial<RouteTrace> & { judgment: string }) {
  return { trace: { request, ...trace } } as RouteLogDetail
}

test.each([
  ['no detail at all', null, ['result']],
  ['only the summary the usage log keeps', { result: { model: 'coder' } }, ['result']],
  [
    'a live classification that was trusted',
    detailOf({ judgment: 'live', trust, ladder, effort }),
    ['request', 'judgment', 'trust', 'selection', 'effort', 'result'],
  ],
  [
    'a cached classification checked against the previous task',
    detailOf({ judgment: 'cache', trust, continuation, ladder, effort }),
    ['request', 'judgment', 'trust', 'continuation', 'selection', 'effort', 'result'],
  ],
  [
    'a classifier failure, whose untrusted answer still decided the route',
    detailOf({
      judgment: 'live_failed',
      trust: { reason: 'no_decision', threshold: 0.5 },
      ladder,
    }),
    ['request', 'judgment', 'trust', 'selection', 'result'],
  ],
  [
    'a background request, which is never classified and so never trusted',
    detailOf({
      judgment: 'auxiliary',
      trust: { reason: 'no_decision', threshold: 0.5 },
      ladder,
    }),
    ['request', 'judgment', 'selection', 'result'],
  ],
  [
    'a sub-agent task that followed its session',
    detailOf({ judgment: 'session', ladder }),
    ['request', 'judgment', 'selection', 'result'],
  ],
  [
    'a request no model could serve',
    detailOf({ judgment: 'unroutable' }),
    ['request', 'judgment', 'result'],
  ],
])('%s lists exactly the steps it went through', (_name, detail, expected) => {
  expect(routeProcessSteps(detail as RouteLogDetail | null)).toEqual(expected)
})
