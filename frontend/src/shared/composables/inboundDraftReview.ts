import type { Inbound } from '@/types/inbounds'
import { createInbound } from '@/types/inbounds'
import type { InboundDraftStatus } from '@/store/modules/data'

export function canReviewInboundDraft(draft: InboundDraftStatus | undefined | null): boolean {
  if (isRetiredSelfStealDraft(draft)) return false
  return draft?.status === 'review_required' && !!draft.payload?.inboundCandidate
}

export function inboundFromDraft(draft: InboundDraftStatus | undefined | null): Inbound | null {
  if (isRetiredSelfStealDraft(draft)) return null
  const candidate = draft?.payload?.inboundCandidate
  const type = String(candidate?.type || draft?.inboundType || '')
  if (!candidate || !type) return null
  return createInbound(<any>type, <any>{
    ...candidate,
    id: 0,
    tls_id: Number(candidate.tls_id ?? 0),
  })
}

function isRetiredSelfStealDraft(draft: InboundDraftStatus | undefined | null): boolean {
  return String(draft?.source || '').trim().endsWith(':self-steal')
}
