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
import { expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { RankingViewer } from '../ranking-viewer'

function mockRanking() {
  return vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        current: true,
        taxonomy_version: '3',
        sources: {},
        profiles: [
          {
            profile: 'code.build',
            models: [
              { model: 'claude-opus-5-5', score: 0.9734 },
              { model: 'private-model', score: null },
            ],
          },
          { profile: 'general', models: [{ model: 'gpt-6.1-terra', score: 0.9 }] },
        ],
      },
    },
  })
}

function renderViewer() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(
    <QueryClientProvider client={client}>
      <RankingViewer />
    </QueryClientProvider>
  )
}

test('the ranking is not fetched until the viewer is opened', async () => {
  const get = mockRanking()
  const user = userEvent.setup()
  renderViewer()

  expect(get).not.toHaveBeenCalled()
  await user.click(screen.getByRole('button', { name: 'Show ranking' }))

  await waitFor(() =>
    expect(get).toHaveBeenCalledWith('/api/autorouter/ranking')
  )
})

test('the first profile is listed in rank order with four-decimal scores and a dash for a model without one', async () => {
  mockRanking()
  const user = userEvent.setup()
  renderViewer()

  await user.click(screen.getByRole('button', { name: 'Show ranking' }))

  const rows = await screen.findAllByRole('row')
  const body = rows.slice(1)
  expect(body).toHaveLength(2)
  expect(within(body[0]).getByText('claude-opus-5-5')).toBeVisible()
  expect(within(body[0]).getByText('0.9734')).toBeVisible()
  expect(within(body[1]).getByText('private-model')).toBeVisible()
  expect(within(body[1]).getByText('-')).toBeVisible()
})

test('hiding the viewer removes the table', async () => {
  mockRanking()
  const user = userEvent.setup()
  renderViewer()

  await user.click(screen.getByRole('button', { name: 'Show ranking' }))
  await screen.findByText('claude-opus-5-5')
  await user.click(screen.getByRole('button', { name: 'Hide ranking' }))

  expect(screen.queryByText('claude-opus-5-5')).not.toBeInTheDocument()
})
