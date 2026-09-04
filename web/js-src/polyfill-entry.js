// Must be loaded via its own <script> tag BEFORE bundle.js
// (consolidation_sign.html already does this in order). hdkey/bip39's
// dependency tree assumes a Node-like environment (Buffer, process) that
// plain browsers don't have — 驗證結論-04/05/06 all hit the same issue:
// assigning window.Buffer/window.process from WITHIN the same module graph
// as the code that needs them executes too late (ES module evaluation
// order), so this has to be a separate bundle that runs to completion
// first. This file has no logic of its own beyond that assignment.
import { Buffer } from 'buffer';
import process from 'process';

window.Buffer = Buffer;
window.process = process;
