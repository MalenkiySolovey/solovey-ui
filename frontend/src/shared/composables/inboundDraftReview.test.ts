import { describe, expect, it } from 'vitest'
import { canReviewInboundDraft, inboundFromDraft } from './inboundDraftReview'

describe('inbound draft review handoff', () => {
  it('opens only review-required drafts with an inbound candidate', () => {
    expect(canReviewInboundDraft({
      id: 1,
      source: 'fallback-html:self-steal',
      sourceRef: 'site/1',
      status: 'blocked',
      inboundType: 'vless',
      tag: 'blocked',
      payload: { inboundCandidate: { type: 'vless' } },
      expiresAt: 0,
    })).toBe(false)

    expect(canReviewInboundDraft({
      id: 2,
      source: 'fallback-html:self-steal',
      sourceRef: 'site/1',
      status: 'review_required',
      inboundType: 'vless',
      tag: 'ready',
      payload: { inboundCandidate: { type: 'vless' } },
      expiresAt: 0,
    })).toBe(true)
  })

  it('normalizes a draft candidate into an unsaved inbound', () => {
    const inbound = inboundFromDraft({
      id: 2,
      source: 'fallback-html:self-steal',
      sourceRef: 'site/1',
      status: 'review_required',
      inboundType: 'vless',
      tag: 'ready',
      payload: {
        inboundCandidate: {
          type: 'vless',
          tag: 'fallback-html-site-1',
          listen: '0.0.0.0',
          listen_port: 443,
        },
      },
      expiresAt: 0,
    })

    expect(inbound).toMatchObject({
      id: 0,
      type: 'vless',
      tag: 'fallback-html-site-1',
      listen: '0.0.0.0',
      listen_port: 443,
      tls_id: 0,
    })
  })
})
