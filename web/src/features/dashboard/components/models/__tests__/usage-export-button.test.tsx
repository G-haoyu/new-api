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
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { toast } from 'sonner'
import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  test,
  vi,
} from 'vitest'

import { getUsageAnalytics } from '@/features/dashboard/api'
import type {
  DashboardFilters,
  UsageAnalyticsGroupBy,
} from '@/features/dashboard/types'

import { UsageExportButton } from '../usage-export-button'

vi.mock('@/features/dashboard/api', () => ({
  getUsageAnalytics: vi.fn(),
}))

vi.mock('sonner', () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}))

type AnalyticsResponse = Awaited<ReturnType<typeof getUsageAnalytics>>

const START = new Date(2026, 0, 5, 8, 0)
const END = new Date(2026, 1, 3, 20, 0)

const FILTERS: DashboardFilters = {
  start_timestamp: START,
  end_timestamp: END,
  time_granularity: 'day',
  username: 'alice',
}

const EMPTY_RESPONSE: AnalyticsResponse = {
  success: true,
  data: { summary: null, rows: [] },
}

const EXPORT_TIMEOUT_MS = 15_000

const originalCreateObjectURL = URL.createObjectURL
const originalRevokeObjectURL = URL.revokeObjectURL
const createObjectURL = vi.fn((_blob: Blob) => 'blob:usage-export')
const revokeObjectURL = vi.fn((_url: string) => undefined)
let downloads: Array<{ download: string; href: string }> = []

function exportButton() {
  return screen.getByRole('button', { name: /^Export/ })
}

function requestedGroupings(): UsageAnalyticsGroupBy[] {
  return vi
    .mocked(getUsageAnalytics)
    .mock.calls.map(([params]) => params.group_by)
}

beforeAll(async () => {
  // Preload exceljs to avoid timing out its first import under jsdom.
  await import('exceljs')
}, 60_000)

beforeEach(() => {
  downloads = []
  URL.createObjectURL = createObjectURL
  URL.revokeObjectURL = revokeObjectURL
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(
    function (this: HTMLAnchorElement) {
      downloads.push({ download: this.download, href: this.href })
    }
  )
})

afterEach(() => {
  URL.createObjectURL = originalCreateObjectURL
  URL.revokeObjectURL = originalRevokeObjectURL
  createObjectURL.mockClear()
  revokeObjectURL.mockClear()
})

describe('UsageExportButton', () => {
  test('while the requests are pending the button is disabled and busy', async () => {
    const user = userEvent.setup()
    const pending: Array<(response: AnalyticsResponse) => void> = []
    vi.mocked(getUsageAnalytics).mockImplementation(
      () =>
        new Promise<AnalyticsResponse>((resolve) => {
          pending.push(resolve)
        })
    )
    render(<UsageExportButton filters={FILTERS} />)

    await user.click(exportButton())

    expect(exportButton()).toBeDisabled()
    expect(exportButton()).toHaveAttribute('aria-busy', 'true')
    for (const resolve of pending) resolve(EMPTY_RESPONSE)
    await waitFor(() => expect(exportButton()).toBeEnabled(), {
      timeout: EXPORT_TIMEOUT_MS,
    })
    expect(exportButton()).toHaveAttribute('aria-busy', 'false')
  })

  test('exporting requests all four groupings in parallel with the current range and user', async () => {
    const user = userEvent.setup()
    const pending: Array<(response: AnalyticsResponse) => void> = []
    vi.mocked(getUsageAnalytics).mockImplementation(
      () =>
        new Promise<AnalyticsResponse>((resolve) => {
          pending.push(resolve)
        })
    )
    render(<UsageExportButton filters={FILTERS} />)

    await user.click(exportButton())

    expect(requestedGroupings()).toEqual([
      'model',
      'user',
      'user_model',
      'model_channel',
    ])
    for (const [params, isAdmin] of vi.mocked(getUsageAnalytics).mock.calls) {
      expect(params).toMatchObject({
        start_timestamp: START.getTime() / 1000,
        end_timestamp: END.getTime() / 1000,
        username: 'alice',
      })
      expect(isAdmin).toBe(true)
    }
    for (const resolve of pending) resolve(EMPTY_RESPONSE)
    await waitFor(() => expect(exportButton()).toBeEnabled(), {
      timeout: EXPORT_TIMEOUT_MS,
    })
  })

  test('a successful export downloads one xlsx file named after the range', async () => {
    const user = userEvent.setup()
    vi.mocked(getUsageAnalytics).mockResolvedValue(EMPTY_RESPONSE)
    render(<UsageExportButton filters={FILTERS} />)

    await user.click(exportButton())

    await waitFor(() => expect(downloads).toHaveLength(1), {
      timeout: EXPORT_TIMEOUT_MS,
    })
    expect(downloads[0]).toEqual({
      download: 'usage-20260105-20260203.xlsx',
      href: 'blob:usage-export',
    })
    expect(createObjectURL.mock.calls[0]?.[0].type).toBe(
      'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet'
    )
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:usage-export')
    expect(vi.mocked(toast.error)).not.toHaveBeenCalled()
  })

  test('a failed grouping request shows the export error and downloads nothing', async () => {
    const user = userEvent.setup()
    vi.mocked(getUsageAnalytics).mockImplementation(async (params) =>
      params.group_by === 'user_model'
        ? { success: false, message: 'range too long' }
        : EMPTY_RESPONSE
    )
    render(<UsageExportButton filters={FILTERS} />)

    await user.click(exportButton())

    await waitFor(() =>
      expect(vi.mocked(toast.error)).toHaveBeenCalledWith('Export failed')
    )
    await waitFor(() => expect(exportButton()).toBeEnabled())
    expect(downloads).toHaveLength(0)
    expect(createObjectURL).not.toHaveBeenCalled()
  })
})
