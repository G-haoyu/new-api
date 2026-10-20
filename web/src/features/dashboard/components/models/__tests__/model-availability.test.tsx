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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { getModelAvailability } from '@/features/dashboard/api'
import type { ModelAvailabilityRow } from '@/features/dashboard/types'

import { ModelAvailability } from '../model-availability'

vi.mock('@/features/dashboard/api', () => ({
  getModelAvailability: vi.fn(),
}))

const NOW = 1_753_964_400 // minute-aligned Unix seconds

function availabilityRow(
  modelName: string,
  success: number,
  streamErrors: number,
  failed: number
): ModelAvailabilityRow {
  return {
    model_name: modelName,
    success_count: success,
    stream_error_count: streamErrors,
    failed_count: failed,
    total_tokens: (success + streamErrors) * 100,
    last_called_at: NOW - 30,
  }
}

const queryClients: QueryClient[] = []

function renderPanel(
  models: ModelAvailabilityRow[] | null,
  options: { errorLogEnabled?: boolean } = {}
) {
  vi.mocked(getModelAvailability).mockImplementation(async (minutes = 10) =>
    models === null
      ? { success: false, message: 'availability unavailable' }
      : {
          success: true,
          data: {
            minutes,
            start_timestamp: NOW - minutes * 60,
            end_timestamp: NOW,
            error_log_enabled: options.errorLogEnabled ?? true,
            models,
          },
        }
  )
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  queryClients.push(queryClient)
  render(
    <QueryClientProvider client={queryClient}>
      <ModelAvailability />
    </QueryClientProvider>
  )
}

function bodyRows() {
  return screen.getAllByRole('row').slice(1)
}

afterEach(() => {
  for (const client of queryClients.splice(0)) client.clear()
})

describe('ModelAvailability', () => {
  test('rows are ranked by confidence with a status badge per model', async () => {
    renderPanel([
      availabilityRow('model-a', 3, 0, 0),
      availabilityRow('model-b', 200, 0, 0),
      availabilityRow('model-c', 45, 3, 2),
    ])

    await screen.findByRole('table')

    const summary = bodyRows().map((row) => {
      const cells = within(row).getAllByRole('cell')
      return [cells[0].textContent, cells[1].textContent, cells[2].textContent]
    })
    expect(summary).toEqual([
      ['1', 'model-b', 'Stable'],
      ['2', 'model-c', 'Degraded'],
      ['3', 'model-a', 'Insufficient samples'],
    ])
  })

  test('overview shows the top model and the call breakdown', async () => {
    renderPanel([
      availabilityRow('model-a', 3, 0, 0),
      availabilityRow('model-b', 200, 0, 0),
      availabilityRow('model-c', 45, 3, 2),
    ])

    await screen.findByRole('table')

    expect(screen.getByTitle('model-b')).toBeInTheDocument()
    expect(screen.getByTitle('100% · Stable')).toBeInTheDocument()
    expect(screen.getByTitle('253')).toBeInTheDocument()
    expect(
      screen.getByTitle('248 succeeded · 3 interrupted · 2 failed')
    ).toBeInTheDocument()
  })

  test('choosing the 1 hour window requests 60 minutes and marks it pressed', async () => {
    const user = userEvent.setup()
    renderPanel([availabilityRow('model-b', 200, 0, 0)])
    await screen.findByRole('table')

    await user.click(screen.getByRole('button', { name: '1 hour' }))

    await waitFor(() =>
      expect(vi.mocked(getModelAvailability)).toHaveBeenLastCalledWith(60)
    )
    expect(screen.getByRole('button', { name: '1 hour' })).toHaveAttribute(
      'aria-pressed',
      'true'
    )
    expect(screen.getByRole('button', { name: '10 minutes' })).toHaveAttribute(
      'aria-pressed',
      'false'
    )
  })

  test('a window without calls shows the empty table message', async () => {
    renderPanel([])

    expect(
      await screen.findByText('No model calls in this window')
    ).toBeInTheDocument()
  })

  test('a failed request shows the load error instead of the table', async () => {
    renderPanel(null)

    expect(
      await screen.findByText('Unable to load model availability')
    ).toBeInTheDocument()
    expect(screen.queryByRole('table')).toBeNull()
  })

  test('disabled error logging shows a notice that failures are not counted', async () => {
    renderPanel([availabilityRow('model-b', 200, 0, 0)], {
      errorLogEnabled: false,
    })

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Error logging is disabled, so failed requests are not counted here. Enable ERROR_LOG_ENABLED for accurate availability.'
    )
    expect(screen.getByRole('table')).toBeInTheDocument()
  })

  test('enabled error logging shows no failure-counting notice', async () => {
    renderPanel([availabilityRow('model-b', 200, 0, 0)], {
      errorLogEnabled: true,
    })

    await screen.findByRole('table')

    expect(screen.queryByRole('alert')).toBeNull()
  })

  test('the per-model rate column is labelled Availability, not Success rate', async () => {
    renderPanel([availabilityRow('model-b', 200, 0, 0)])

    await screen.findByRole('table')

    expect(
      screen.getByRole('columnheader', { name: 'Availability' })
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('columnheader', { name: 'Success rate' })
    ).toBeNull()
  })
})
