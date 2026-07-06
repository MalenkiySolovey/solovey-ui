import type { Inbound } from '@/types/inbounds'
import { createInbound } from '@/types/inbounds'
import type { InboundDraftStatus } from '@/store/modules/data'

export function canReviewInboundDraft(draft: InboundDraftStatus | undefined | null): boolean {
  return draft?.status === 'review_required' && !!draft.payload?.inboundCandidate
}

export function inboundFromDraft(draft: InboundDraftStatus | undefined | null): Inbound | null {
  const candidate = draft?.payload?.inboundCandidate
  const type = String(candidate?.type || draft?.inboundType || '')
  if (!candidate || !type) return null
  return createInbound(<any>type, <any>{
    ...candidate,
    id: 0,
    tls_id: Number(candidate.tls_id ?? 0),
  })
}
