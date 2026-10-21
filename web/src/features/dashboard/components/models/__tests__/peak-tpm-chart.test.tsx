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
import { render, screen } from '@testing-library/react'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { getPeakTpm } from '@/features/dashboard/api'
import type { DashboardFilters, PeakTpmPoint } from '@/features/dashboard/types'

import { PeakTpmChart } from '../peak-tpm-chart'

vi.mock('@/features/dashboard/api', () => ({
  getPeakTpm: vi.fn(),
}))

// Canvas rendering is not available in jsdom.
vi.mock('@visactor/react-vchart', () => ({
  VChart: () => null,
}))
vi.mock('@visactor/vchart', () => ({
  ThemeManager: { setCurrentTheme: () => undefined },
}))

const START = new Date(2025, 6, 31, 0, 0)
const END = new Date(2025, 6, 31, 12, 0)

const FILTERS: DashboardFilters = {
  start_timestamp: START,
  end_timestamp: END,
  time_granularity: 'hour',
  username: ' alice ',
}

const queryClients: QueryClient[] = []

function renderChart(points: PeakTpmPoint[] | null) {
  vi.mocked(getPeakTpm).mockResolvedValue(
    points === null
      ? { success: false, message: 'peak tpm unavailable' }
      : { success: true, data: points }
  )
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  queryClients.push(queryClient)
  render(
    <QueryClientProvider client={queryClient}>
      <PeakTpmChart filters={FILTERS} />
    </QueryClientProvider>
  )
}

function hourTs(hour: number): number {
  return new Date(2025, 6, 31, hour).getTime() / 1000
}

afterEach(() => {
  for (const client of queryClients.splice(0)) client.clear()
})

describe('PeakTpmChart', () => {
  test('requests the dashboard range with the trimmed username filter', async () => {
    renderChart([])

    await screen.findByText('No data available')

    expect(vi.mocked(getPeakTpm)).toHaveBeenCalledWith({
      start_timestamp: START.getTime() / 1000,
      end_timestamp: END.getTime() / 1000,
      username: 'alice',
    })
  })

  test('a range without traffic shows the empty state', async () => {
    renderChart([])

    expect(await screen.findByText('No data available')).toBeInTheDocument()
    expect(screen.queryByText('Peak')).toBeNull()
  })

  test('a failed request shows the load error', async () => {
    renderChart(null)

    expect(
      await screen.findByText('Unable to load peak TPM')
    ).toBeInTheDocument()
  })

  test('the header shows the highest hourly peak in the range', async () => {
    renderChart([
      { hour: hourTs(2), peak_tpm: 320 },
      { hour: hourTs(5), peak_tpm: 875 },
      { hour: hourTs(9), peak_tpm: 140 },
    ])

    expect(await screen.findByText('875')).toBeInTheDocument()
    expect(screen.getByText('Peak')).toBeInTheDocument()
    expect(screen.queryByText('No data available')).toBeNull()
  })
})
