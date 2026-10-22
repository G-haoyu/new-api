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
import { fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, test, vi } from 'vitest'

import { DEFAULT_DASHBOARD_CHART_PREFERENCES } from '@/features/dashboard/constants'
import type { DashboardFilters } from '@/features/dashboard/types'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import { ModelsFilter } from '../models-filter-dialog'

const DAY_MS = 86_400_000

function openDialogWithRange(
  role: number,
  start: Date,
  end: Date | undefined,
  onFilterChange: (filters: DashboardFilters) => void = () => undefined
) {
  useAuthStore.getState().auth.setUser({ id: 1, username: 'tester', role })
  render(
    <ModelsFilter
      preferences={DEFAULT_DASHBOARD_CHART_PREFERENCES}
      currentFilters={{
        start_timestamp: start,
        end_timestamp: end,
        time_granularity: 'day',
        username: '',
      }}
      onFilterChange={onFilterChange}
      onReset={() => undefined}
    />
  )
  fireEvent.click(screen.getByRole('button', { name: 'Filter' }))
}

function openFilterDialog(
  role: number,
  rangeDays: number,
  onFilterChange: (filters: DashboardFilters) => void = () => undefined
) {
  const end = new Date(2025, 6, 31, 12, 0)
  openDialogWithRange(
    role,
    new Date(end.getTime() - rangeDays * DAY_MS),
    end,
    onFilterChange
  )
}

// Clearing the end date restores the dashboard's default end time.
function openDialogAndClearEnd(role: number, startDaysAgo: number) {
  const start = new Date(Date.now() - startDaysAgo * DAY_MS)
  openDialogWithRange(role, start, new Date(start.getTime() + 20 * DAY_MS))
  expect(screen.queryByRole('alert')).toBeNull()
  fireEvent.click(screen.getAllByRole('button', { name: 'Clear' })[1])
}

afterEach(() => {
  useAuthStore.getState().auth.setUser(null)
})

describe('ModelsFilter range limits', () => {
  test('admin users get the 60 and 90 day quick ranges', () => {
    openFilterDialog(ROLE.ADMIN, 7)

    expect(screen.getByRole('button', { name: '60 Days' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '90 Days' })).toBeInTheDocument()
  })

  test('regular users only get quick ranges up to 29 days', () => {
    openFilterDialog(ROLE.USER, 7)

    expect(screen.getByRole('button', { name: '29 Days' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '60 Days' })).toBeNull()
    expect(screen.queryByRole('button', { name: '90 Days' })).toBeNull()
  })

  test('admin range over 92 days disables apply and shows the limit', () => {
    openFilterDialog(ROLE.ADMIN, 100)

    expect(screen.getByRole('alert')).toHaveTextContent(
      'Time range cannot exceed 92 days'
    )
    expect(screen.getByRole('button', { name: 'Apply Filters' })).toBeDisabled()
  })

  test('regular user range over 30 days disables apply and shows the limit', () => {
    openFilterDialog(ROLE.USER, 45)

    expect(screen.getByRole('alert')).toHaveTextContent(
      'Time range cannot exceed 30 days'
    )
    expect(screen.getByRole('button', { name: 'Apply Filters' })).toBeDisabled()
  })

  test('regular user clearing the end of a range starting 60 days ago disables apply and shows the limit', () => {
    openDialogAndClearEnd(ROLE.USER, 60)

    expect(screen.getByRole('alert')).toHaveTextContent(
      'Time range cannot exceed 30 days'
    )
    expect(screen.getByRole('button', { name: 'Apply Filters' })).toBeDisabled()
  })

  test('admin clearing the end of a range starting 100 days ago disables apply and shows the limit', () => {
    openDialogAndClearEnd(ROLE.ADMIN, 100)

    expect(screen.getByRole('alert')).toHaveTextContent(
      'Time range cannot exceed 92 days'
    )
    expect(screen.getByRole('button', { name: 'Apply Filters' })).toBeDisabled()
  })

  test('admin picking the 90 day quick range applies a weekly 90 day range', () => {
    const onFilterChange = vi.fn()
    openFilterDialog(ROLE.ADMIN, 100, onFilterChange)

    fireEvent.click(screen.getByRole('button', { name: '90 Days' }))

    expect(screen.queryByRole('alert')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Apply Filters' }))
    const applied = onFilterChange.mock.calls[0][0] as DashboardFilters
    const startMs = applied.start_timestamp?.getTime() ?? 0
    const endMs = applied.end_timestamp?.getTime() ?? 0
    expect(endMs - startMs).toBe(90 * DAY_MS)
    expect(applied.time_granularity).toBe('week')
  })
})
