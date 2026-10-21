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
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, test } from 'vitest'

import { usageLogSchema, type UsageLog } from '../../data/schema'
import type { ChannelAffinityInfo } from '../../types'
import { useCommonLogsColumns } from '../columns/common-logs-columns'
import { UsageLogsProvider, useUsageLogsContext } from '../usage-logs-provider'

function UserEmailTable(props: { log: UsageLog }) {
  const columns = useCommonLogsColumns(true, false)
  const { sensitiveVisible, setSensitiveVisible } = useUsageLogsContext()
  const table = useReactTable({
    data: [props.log],
    columns,
    getCoreRowModel: getCoreRowModel(),
  })
  const row = table.getRowModel().rows[0]
  const cell = row.getAllCells().find((cell) => cell.column.id === 'user')
  if (!cell) throw new Error('User column is missing')

  return (
    <>
      <button
        type='button'
        onClick={() => setSensitiveVisible(!sensitiveVisible)}
      >
        Toggle sensitive data
      </button>
      <table>
        <tbody>
          <tr>
            <td>{flexRender(cell.column.columnDef.cell, cell.getContext())}</td>
          </tr>
        </tbody>
      </table>
    </>
  )
}

function renderUser(email: string, affinity?: ChannelAffinityInfo) {
  const log = usageLogSchema.parse({
    id: 1,
    user_id: 1,
    created_at: 1,
    type: 2,
    content: '',
    username: 'iself',
    iself_email: email,
    other: JSON.stringify({ admin_info: { channel_affinity: affinity } }),
  })

  render(
    <UsageLogsProvider>
      <UserEmailTable log={log} />
    </UsageLogsProvider>
  )
}

describe('usage log user email', () => {
  test('shows a recorded email without channel affinity', () => {
    renderUser('alice@example.com')
    expect(screen.getByText('alice@example.com')).toBeInTheDocument()
  })

  test('prefers a recorded email over a different affinity hint', () => {
    renderUser('alice@example.com', {
      key_source: 'request_header',
      key_hint: 'previous@example.com',
    })
    expect(screen.getByText('alice@example.com')).toBeInTheDocument()
    expect(screen.queryByText('previous@example.com')).not.toBeInTheDocument()
  })

  test('keeps the header affinity fallback for historical logs', () => {
    renderUser('', {
      key_source: 'request_header',
      key_hint: 'old@example.com',
    })
    expect(screen.getByText('old@example.com')).toBeInTheDocument()
  })

  test('does not display a non-header affinity value as an email', () => {
    renderUser('', { key_source: 'gjson', key_hint: 'session-123' })
    expect(screen.queryByText('session-123')).not.toBeInTheDocument()
  })

  test('hides a non-affinity email when sensitive data is hidden', async () => {
    const user = userEvent.setup()
    renderUser('alice@example.com')
    expect(screen.getByText('alice@example.com')).toBeInTheDocument()
    await user.click(
      screen.getByRole('button', { name: 'Toggle sensitive data' })
    )
    expect(screen.queryByText('alice@example.com')).not.toBeInTheDocument()
  })
})
