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
import { Code, Plus, Table, Trash2 } from 'lucide-react'
import { useEffect, useId, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { JsonCodeEditor } from '@/components/json-code-editor'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'

type CostMultiplierEditorProps = {
  value: string
  onChange: (value: string) => void
  disabled?: boolean
  modelOptions?: string[]
}

type MultiplierRow = {
  id: string
  model: string
  multiplier: string
}

const isValidMultiplier = (value: string): boolean => {
  const multiplier = Number(value)
  return (
    value.trim() !== '' &&
    Number.isFinite(multiplier) &&
    multiplier > 0 &&
    !/^\s*[-+]/.test(value)
  )
}

const parseMultiplier = (raw: unknown): string | null => {
  if (typeof raw !== 'number' || !Number.isFinite(raw) || raw <= 0) {
    return null
  }
  return String(raw)
}

export function CostMultiplierEditor(props: CostMultiplierEditorProps) {
  const { t } = useTranslation()
  const modelListId = useId()
  const [mode, setMode] = useState<'visual' | 'json'>('visual')
  const [rows, setRows] = useState<MultiplierRow[]>([])
  const [jsonValue, setJsonValue] = useState(props.value)
  const [jsonError, setJsonError] = useState<string | null>(null)
  const nextRowIdRef = useRef(0)
  // The JSON this component last emitted via onChange. The value prop echoes
  // it back through the form; re-parsing it would drop in-progress rows whose
  // model or multiplier is still being edited, so only external changes
  // (loading another channel, resetting the form) re-parse.
  const lastEmittedJsonRef = useRef<string | null>(null)

  const createRowId = () => {
    nextRowIdRef.current += 1
    return `multiplier-${nextRowIdRef.current}`
  }

  const invalidRows = useMemo(
    () =>
      rows.filter(
        (row) =>
          row.model.trim() !== '' &&
          (!isValidMultiplier(row.multiplier) ||
            (props.modelOptions &&
              props.modelOptions.length > 0 &&
              !props.modelOptions.includes(row.model.trim())))
      ),
    [rows, props.modelOptions]
  )

  const duplicateModels = useMemo(() => {
    const seen = new Set<string>()
    const duplicates = new Set<string>()
    for (const row of rows) {
      const model = row.model.trim()
      if (!model) continue
      if (seen.has(model)) {
        duplicates.add(model)
      } else {
        seen.add(model)
      }
    }
    return Array.from(duplicates)
  }, [rows])

  const parseJsonToRows = (json: string): boolean => {
    try {
      if (!json.trim()) {
        setRows([])
        setJsonError(null)
        return true
      }
      const parsed = JSON.parse(json)
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
        setJsonError(t('Cost multiplier must be a valid JSON object'))
        return false
      }
      const entries = Object.entries(parsed)
      const invalidEntry = entries.find(
        ([model, multiplier]) =>
          !model.trim() || parseMultiplier(multiplier) === null
      )
      if (invalidEntry) {
        setJsonError(
          t('Cost multiplier values must be positive numbers')
        )
        return false
      }
      setRows(
        entries.map(([model, multiplier]) => ({
          id: createRowId(),
          model,
          multiplier: parseMultiplier(multiplier) ?? '',
        }))
      )
      setJsonError(null)
      return true
    } catch (_error) {
      setJsonError(t('Cost multiplier must be valid JSON format'))
      return false
    }
  }

  // Parse JSON to rows when the value changes externally (not when our own
  // onChange output echoes back).
  useEffect(() => {
    if (props.value === lastEmittedJsonRef.current) return
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setJsonValue(props.value)
    parseJsonToRows(props.value)
  }, [props.value])

  const convertRowsToJson = (updatedRows: MultiplierRow[]): string => {
    if (updatedRows.length === 0) {
      return ''
    }
    const obj: Record<string, number> = {}
    updatedRows.forEach((row) => {
      if (row.model.trim() && isValidMultiplier(row.multiplier)) {
        obj[row.model.trim()] = Number(row.multiplier)
      }
    })
    return JSON.stringify(obj, null, 2)
  }

  const syncRows = (updatedRows: MultiplierRow[]) => {
    setRows(updatedRows)
    const json = convertRowsToJson(updatedRows)
    setJsonError(null)
    setJsonValue(json)
    lastEmittedJsonRef.current = json
    props.onChange(json)
  }

  const handleAddRow = () => {
    const newRow: MultiplierRow = {
      id: createRowId(),
      model: '',
      multiplier: '',
    }
    syncRows([...rows, newRow])
  }

  const handleDeleteRow = (id: string) => {
    syncRows(rows.filter((row) => row.id !== id))
  }

  const handleRowChange = (
    id: string,
    field: 'model' | 'multiplier',
    newValue: string
  ) => {
    const updatedRows = rows.map((row) => {
      if (row.id !== id) return row
      if (field === 'model') {
        // Picking a model is the signal to start editing its multiplier;
        // pre-fill 1.0 so one keystroke less is needed for the common case.
        const shouldPrefill =
          newValue.trim() !== '' && row.multiplier.trim() === ''
        return { ...row, model: newValue, multiplier: shouldPrefill ? '1.0' : row.multiplier }
      }
      return { ...row, multiplier: newValue }
    })
    syncRows(updatedRows)
  }

  const handleJsonChange = (newJson: string) => {
    setJsonValue(newJson)
    lastEmittedJsonRef.current = newJson
    props.onChange(newJson)
    parseJsonToRows(newJson)
  }

  const handleModeChange = (nextMode: string) => {
    if (nextMode !== 'visual' && nextMode !== 'json') return
    if (nextMode === 'json') {
      const json = convertRowsToJson(rows)
      setJsonValue(json)
      lastEmittedJsonRef.current = json
      props.onChange(json)
      setMode('json')
      return
    }
    parseJsonToRows(jsonValue)
    setMode('visual')
  }

  return (
    <div className='space-y-2'>
      <Tabs value={mode} onValueChange={handleModeChange} className='space-y-2'>
        <TabsList>
          <TabsTrigger value='visual'>
            <Table className='h-4 w-4' aria-hidden='true' />
            {t('Visual')}
          </TabsTrigger>
          <TabsTrigger value='json'>
            <Code className='h-4 w-4' aria-hidden='true' />
            {t('JSON')}
          </TabsTrigger>
        </TabsList>

        {jsonError && (
          <Alert variant='destructive'>
            <AlertDescription>{jsonError}</AlertDescription>
          </Alert>
        )}

        {duplicateModels.length > 0 && (
          <Alert variant='destructive'>
            <AlertDescription>
              {t('Duplicate model(s): {{models}}', {
                models: duplicateModels.join(', '),
              })}
            </AlertDescription>
          </Alert>
        )}

        {invalidRows.length > 0 && (
          <Alert variant='destructive'>
            <AlertDescription>
              {t(
                'Each row needs a model this channel actually serves upstream (mapping target or unmapped channel model) and a positive multiplier.'
              )}
            </AlertDescription>
          </Alert>
        )}

        <TabsContent value='visual' className='space-y-2'>
          {rows.length > 0 ? (
            <div className='space-y-2'>
              <div className='grid grid-cols-[1fr_160px_auto] gap-2 text-sm font-medium'>
                <div>{t('Upstream Model')}</div>
                <div>{t('Multiplier')}</div>
                <div className='w-10'></div>
              </div>
              {rows.map((row) => (
                <div key={row.id} className='grid grid-cols-[1fr_160px_auto] gap-2'>
                  <Input
                    value={row.model}
                    onChange={(e) =>
                      handleRowChange(row.id, 'model', e.target.value)
                    }
                    placeholder='gpt-4o-2024-08'
                    disabled={props.disabled}
                    list={modelListId}
                  />
                  <Input
                    value={row.multiplier}
                    onChange={(e) =>
                      handleRowChange(row.id, 'multiplier', e.target.value)
                    }
                    placeholder='1.5'
                    inputMode='decimal'
                    disabled={props.disabled}
                    aria-invalid={
                      row.model.trim() !== '' &&
                      !isValidMultiplier(row.multiplier)
                    }
                  />
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon'
                    onClick={() => handleDeleteRow(row.id)}
                    disabled={props.disabled}
                    className='h-10 w-10'
                    aria-label={t('Delete multiplier')}
                  >
                    <Trash2 className='h-4 w-4' aria-hidden='true' />
                  </Button>
                </div>
              ))}
            </div>
          ) : (
            <div className='text-muted-foreground flex h-24 items-center justify-center rounded-md border border-dashed text-sm'>
              {t(
                'No cost multipliers configured. Click "Add Multiplier" to get started.'
              )}
            </div>
          )}
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={handleAddRow}
            disabled={props.disabled}
            className='w-full'
          >
            <Plus className='mr-2 h-4 w-4' />
            {t('Add Multiplier')}
          </Button>
        </TabsContent>
        <TabsContent value='json'>
          <JsonCodeEditor
            value={jsonValue}
            onChange={handleJsonChange}
            placeholder={t('{"gpt-4o-2024-08": 1.5}')}
            disabled={props.disabled}
            className={jsonError ? 'border-destructive' : undefined}
            aria-invalid={Boolean(jsonError)}
            ariaLabel={t('Model Cost Multiplier')}
          />
        </TabsContent>
      </Tabs>

      {props.modelOptions && props.modelOptions.length > 0 && (
        <datalist id={modelListId}>
          {props.modelOptions.map((model) => (
            <option key={model} value={model} />
          ))}
        </datalist>
      )}
    </div>
  )
}
