/**
 * P7-RUN-001c / P7.12 — the capability advertisement as a rule, not a comment.
 *
 * The execution contract says a provider must advertise what it keeps and a
 * plan that needs anything else must be refused at admission. These tests pin
 * the four derivations that rule rests on: what a DAG requires, what an
 * advertisement is missing, which states are final, and which state means
 * "a human terminated it".
 */

import { describe, it, expect } from 'vitest';
import {
  TERMINAL_STATES,
  WORKFLOW_CAPABILITIES,
  dagRequires,
  isCancelled,
  isTerminalState,
  missingCapabilities,
  type CapabilityAdvertisement,
} from './workflowCapabilities';

describe('workflowCapabilities — the deployed advertisement', () => {
  it('advertises all four guarantees the runtime implements', () => {
    expect(WORKFLOW_CAPABILITIES).toEqual({
      cancel: true,
      pause: true,
      longSleep: true,
      history: true,
    });
  });

  it('derives longSleep from a sleep node and nothing from the other kinds', () => {
    expect(dagRequires({ nodes: [{ type: 'sleep' }, { type: 'ai' }] })).toEqual(['longSleep']);
    expect(dagRequires({ nodes: [{ type: 'trigger' }, { type: 'http' }, { type: 'ai' }] })).toEqual([]);
  });

  it('derives nothing from a missing or empty node list', () => {
    expect(dagRequires(null)).toEqual([]);
    expect(dagRequires({})).toEqual([]);
    expect(dagRequires({ nodes: [] })).toEqual([]);
  });

  it('reports nothing missing when every requirement is advertised', () => {
    expect(missingCapabilities([])).toEqual([]);
    expect(missingCapabilities(['longSleep', 'cancel'])).toEqual([]);
  });

  it('reports a requirement the advertisement does not keep, rather than degrading', () => {
    // A runtime that cannot sleep durably must refuse a sleep-bearing plan.
    const withoutLongSleep: CapabilityAdvertisement = { ...WORKFLOW_CAPABILITIES, longSleep: false };
    expect(missingCapabilities(['longSleep'], withoutLongSleep)).toEqual(['longSleep']);
    expect(missingCapabilities(['longSleep', 'cancel'], withoutLongSleep)).toEqual(['longSleep']);
  });
});

describe('workflowCapabilities — finality', () => {
  it('treats exactly the three terminal states as final', () => {
    expect([...TERMINAL_STATES]).toEqual(['succeeded', 'failed', 'cancelled']);
    for (const state of TERMINAL_STATES) {
      expect(isTerminalState(state)).toBe(true);
    }
  });

  it('treats live states as non-final', () => {
    for (const state of ['pending', 'running', 'paused', '']) {
      expect(isTerminalState(state)).toBe(false);
    }
    expect(isTerminalState(null)).toBe(false);
    expect(isTerminalState(undefined)).toBe(false);
  });

  it('normalizes case and whitespace before deciding', () => {
    expect(isTerminalState(' CANCELLED ')).toBe(true);
    expect(isCancelled(' Cancelled ')).toBe(true);
    expect(isCancelled('cancelled')).toBe(true);
  });

  it('distinguishes cancelled from every other state', () => {
    // The distinction the runtime's catch block depends on: a cancelled run must
    // never be rewritten as failed, so the two cannot share a predicate.
    for (const state of ['succeeded', 'failed', 'pending', 'running', 'paused', null]) {
      expect(isCancelled(state)).toBe(false);
    }
  });
});
