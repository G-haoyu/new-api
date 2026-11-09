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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { handleServerError } from '@/lib/handle-server-error'

import { clearScopeCache } from './api'
import type { AffinityRule } from './types'

interface Props {
  rules: AffinityRule[]
  onClose: () => void
  onCleared: () => void
}

// 按模型 / 用户清绑定。只有键里带了这个维度的规则清得动，所以规则下拉只列满足条件的。
export function ClearScopeDialog(props: Props) {
  const { t } = useTranslation()
  const [model, setModel] = useState('')
  const [userId, setUserId] = useState('')
  const [ruleName, setRuleName] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const modelName = model.trim()
  const userIdText = userId.trim()
  const userIdInvalid = userIdText !== '' && !/^[1-9]\d*$/.test(userIdText)
  const candidates = props.rules.filter(
    (rule) =>
      rule.include_rule_name &&
      rule.name &&
      (rule.include_model_name || rule.include_user_id)
  )

  const handleConfirm = async () => {
    setSubmitting(true)
    try {
      const res = await clearScopeCache({
        model: modelName,
        userId: userIdText ? Number(userIdText) : undefined,
        ruleName,
      })
      if (!res.success) {
        handleServerError(res)
        return
      }
      const deleted = res.data?.deleted ?? 0
      if (deleted > 0) {
        toast.success(t('Cleared {{deleted}} entries', { deleted }))
      } else {
        toast.info(t('No matching cache entries'))
      }
      props.onCleared()
      props.onClose()
    } catch (error) {
      handleServerError(error)
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <ConfirmDialog
      open
      onOpenChange={(open) => !open && props.onClose()}
      title={t('Clear cache by model or user')}
      desc={
        candidates.length > 0
          ? t(
              'Only session rules that keep the model or user in their cache key can be cleared this way. Fill in one or both.'
            )
          : t(
              'No session rule keeps the model or user in its cache key. Enable Include Model or Include User on a rule first.'
            )
      }
      confirmText={t('Clear')}
      handleConfirm={handleConfirm}
      disabled={
        candidates.length === 0 ||
        userIdInvalid ||
        (modelName === '' && userIdText === '')
      }
      isLoading={submitting}
      destructive
    >
      <div className='grid gap-3'>
        <div className='grid gap-1.5'>
          <Label htmlFor='clear-scope-model'>{t('Model name')}</Label>
          <Input
            id='clear-scope-model'
            value={model}
            onChange={(event) => setModel(event.target.value)}
            placeholder='gpt-5'
          />
        </div>
        <div className='grid gap-1.5'>
          <Label htmlFor='clear-scope-user'>{t('User ID')}</Label>
          <Input
            id='clear-scope-user'
            inputMode='numeric'
            value={userId}
            onChange={(event) => setUserId(event.target.value)}
            aria-invalid={userIdInvalid}
            aria-describedby={
              userIdInvalid ? 'clear-scope-user-error' : undefined
            }
          />
          {userIdInvalid && (
            <p
              id='clear-scope-user-error'
              className='text-destructive text-xs'
            >
              {t('User ID must be a positive integer')}
            </p>
          )}
        </div>
        <div className='grid gap-1.5'>
          <Label htmlFor='clear-scope-rule'>{t('Rule')}</Label>
          <NativeSelect
            id='clear-scope-rule'
            className='w-full'
            value={ruleName}
            onChange={(event) => setRuleName(event.target.value)}
          >
            <NativeSelectOption value=''>{t('All rules')}</NativeSelectOption>
            {candidates.map((rule) => (
              <NativeSelectOption key={rule.name} value={rule.name}>
                {rule.name}
              </NativeSelectOption>
            ))}
          </NativeSelect>
        </div>
      </div>
    </ConfirmDialog>
  )
}
