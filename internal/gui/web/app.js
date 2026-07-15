const state = { course: null, module: null, session: null, courses: [], modules: [], moduleWorkspace: null, workspace: null, busy: new Set(), transcriptionController: null };
const $ = selector => document.querySelector(selector);

class APIError extends Error {
  constructor(status, body) { super(body?.error?.message || 'StudyPilot could not complete the request.'); this.status = status; this.code = body?.error?.code || 'internal'; this.recoverable = Boolean(body?.error?.recoverable); }
}

async function api(path, options = {}) {
  const headers = options.body ? {'Content-Type': 'application/json'} : {};
  const response = await fetch(`/api/v1${path}`, {...options, headers: {...headers, ...(options.headers || {})}});
  let value = {};
  try { value = await response.json(); } catch { value = {}; }
  if (!response.ok) throw new APIError(response.status, value);
  return value;
}

function announce(message) { $('#status').textContent = message; }
function clearError() { $('#error').hidden = true; $('#error').textContent = ''; }
function showError(error, suggestion = '') {
  const code = error.code || 'internal';
  const recovery = suggestion || (error.status === 409 ? 'Current state has been refreshed. Review it before retrying.' : error.recoverable ? 'Refresh current state and retry when safe.' : 'Review the current configuration or state.');
  const node = $('#error'); node.textContent = `${friendlyCode(code)}: ${error.message} ${recovery}`; node.hidden = false;
}
function friendlyCode(code) { return String(code).replaceAll('_', ' ').replace(/^./, value => value.toUpperCase()); }
function setLoading(id, loading) { const node = $(id); if (node) node.hidden = !loading; }
function showView(id) { for (const view of ['courses-view','module-view','session-view']) $(`#${view}`).hidden = view !== id; }
function encode(value) { return encodeURIComponent(value); }
function empty(text) { const p = document.createElement('p'); p.textContent = text; return p; }
function textElement(tag, text, className = '') { const node = document.createElement(tag); node.textContent = text; if (className) node.className = className; return node; }

async function route() {
  clearError();
  const parts = location.hash.replace(/^#\/?/, '').split('/').filter(Boolean).map(decodeURIComponent);
  if (parts[0] === 'course' && parts[2] === 'module' && parts[4] === 'session' && parts[5]) return loadSession(parts[1], parts[3], parts[5]);
  if (parts[0] === 'course' && parts[2] === 'module' && parts[3]) return loadModule(parts[1], parts[3]);
  if (parts[0] === 'course' && parts[1]) return chooseCourse(parts[1]);
  return loadCourses();
}

async function loadCourses() {
  showView('courses-view'); $('#modules-panel').hidden = true; $('#module-nav').hidden = true; $('#revision').textContent = 'Revision —';
  setLoading('#courses-loading', true); announce('Loading courses…');
  try {
    const value = await api('/courses'); state.courses = value.courses;
    const list = $('#course-list'); list.replaceChildren();
    for (const course of value.courses) {
      const card = textElement('article', '', 'card');
      const button = document.createElement('button'); button.type = 'button'; button.textContent = course.name; button.addEventListener('click', () => { location.hash = `#/course/${encode(course.id)}`; });
      card.append(button, textElement('p', `Reference: ${course.id}`), textElement('p', `${course.modules} modules · ${course.unfinished_sessions} unfinished sessions`)); list.append(card);
    }
    if (!value.courses.length) list.append(empty('No courses are available. Create one with the StudyPilot CLI, then refresh.'));
    announce('Choose a course.');
  } catch (error) { showError(error); announce('Courses could not be loaded.'); }
  finally { setLoading('#courses-loading', false); }
}

async function chooseCourse(courseID) {
  showView('courses-view'); $('#modules-panel').hidden = false; state.course = courseID; state.module = null; state.session = null;
  setLoading('#modules-loading', true); announce('Loading modules…');
  try {
    const value = await api(`/courses/${encode(courseID)}/modules`); state.modules = value.modules;
    const course = state.courses.find(item => item.id === courseID); $('#modules-title').textContent = course ? `${course.name} modules` : 'Modules';
    const list = $('#module-list'); list.replaceChildren();
    for (const module of value.modules) {
      const card = textElement('article', '', 'card');
      const button = document.createElement('button'); button.type = 'button'; button.textContent = `Module ${module.number}: ${module.name}`; button.addEventListener('click', () => { location.hash = `#/course/${encode(courseID)}/module/${encode(module.id)}`; });
      card.append(button, textElement('p', `${module.sessions} sessions · ${module.unfinished_sessions} unfinished`), textElement('p', `${module.transcript_count} transcripts · ${module.artifact_issues} artifact issues · Notes ${module.module_notes_exists ? 'created' : 'missing'}`)); list.append(card);
    }
    if (!value.modules.length) list.append(empty('This course has no modules.'));
    announce('Choose a module.');
  } catch (error) { showError(error); announce('Modules could not be loaded.'); }
  finally { setLoading('#modules-loading', false); }
}

async function loadModule(courseID, moduleID) {
  showView('module-view'); state.course = courseID; state.module = moduleID; state.session = null; $('#module-nav').hidden = true;
  setLoading('#module-loading', true); announce('Loading module workspace…');
  try {
    const value = await api(`/courses/${encode(courseID)}/modules/${encode(moduleID)}/workspace`); state.moduleWorkspace = value;
    $('#module-title').textContent = `Module ${value.module.number}: ${value.module.name}`;
    $('#module-summary').textContent = `${value.sessions.length} sessions · ${value.module.unfinished_sessions} unfinished · ${value.module.transcript_count} transcripts · ${value.artifact_issues.length} artifact issues`;
    $('#create-module-notes').disabled = value.module.module_notes_exists || state.busy.has('module-notes'); $('#create-module-notes').title = value.module.module_notes_exists ? 'Module notes already exist.' : '';
    renderSessionGroups(value.sessions); renderIssues('#module-issues', [...value.session_issues, ...value.artifact_issues]);
    announce('Module workspace is current.');
  } catch (error) { showError(error); announce('Module workspace could not be loaded.'); }
  finally { setLoading('#module-loading', false); }
}

function renderSessionGroups(sessions) {
  const container = $('#session-groups'); container.replaceChildren();
  for (const status of ['active','interrupted','planned','completed','abandoned']) {
    const values = sessions.filter(item => item.session_status === status);
    if (!values.length) continue;
    const section = document.createElement('section'); section.append(textElement('h3', `${friendlyCode(status)} sessions`));
    const grid = textElement('div', '', 'card-grid');
    for (const session of values) {
      const card = textElement('article', '', 'card'); const button = document.createElement('button'); button.type = 'button'; button.textContent = session.title;
      button.addEventListener('click', () => { location.hash = `#/course/${encode(state.course)}/module/${encode(state.module)}/session/${encode(session.id)}`; });
      card.append(button, textElement('p', `Session ${session.number} · Revision ${session.revision}`), textElement('p', `Capture: ${friendlyCode(session.capture_status)} · ${session.finalized_segments} finalized segments`), textElement('p', `Transcription: ${friendlyCode(session.transcription_status)} · Notes ${session.notes_exists ? 'created' : 'missing'} · ${session.artifact_issues} issues`)); grid.append(card);
    }
    section.append(grid); container.append(section);
  }
  if (!sessions.length) container.append(empty('No sessions yet. Create the first session above.'));
}

async function createSession(event) {
  event.preventDefault(); if (state.busy.has('create-session')) return;
  const title = $('#session-title').value.trim(); if (!title) return;
  state.busy.add('create-session'); $('#create-session').disabled = true; clearError(); announce('Creating session…');
  try {
    const value = await api(`/courses/${encode(state.course)}/modules/${encode(state.module)}/sessions`, {method:'POST', body:JSON.stringify({title})});
    $('#session-title').value = ''; announce('Session created.'); location.hash = `#/course/${encode(state.course)}/module/${encode(state.module)}/session/${encode(value.id)}`;
  } catch (error) { showError(error); announce('Session was not created.'); }
  finally { state.busy.delete('create-session'); $('#create-session').disabled = false; }
}

async function loadSession(courseID = state.course, moduleID = state.module, sessionID = state.session) {
  showView('session-view'); state.course = courseID; state.module = moduleID; state.session = sessionID; $('#module-nav').hidden = false;
  setLoading('#session-loading', true); announce('Loading authoritative session state…');
  try {
    const value = await api(`/sessions/${encode(courseID)}/${encode(moduleID)}/${encode(sessionID)}`); state.workspace = value;
    renderSession(value); announce('Session workspace is current.');
  } catch (error) { showError(error); announce('Session workspace could not be loaded.'); }
  finally { setLoading('#session-loading', false); }
}

function renderSession(value) {
  $('#revision').textContent = `Runtime revision ${value.session.revision}`; $('#session-title-heading').textContent = value.session.title;
  $('#session-breadcrumb').textContent = `${value.course.name} · Module ${value.module.number}: ${value.module.name}`;
  $('#session-identifiers').textContent = `Course ${value.course.id} · Module ${value.module.id} · Session ${value.session.id}`;
  const finalized = value.session.segments.filter(segment => segment.status === 'stopped').length;
  const overview = [['Session', friendlyCode(value.session.session_status)], ['Runtime revision', value.session.revision], ['Capture', captureLabel(value)], ['Finalized segments', finalized], ['Transcription', friendlyCode(value.session.transcription_status)], ['Session notes', value.notes.session_exists ? 'Created' : 'Missing'], ['Artifact revision', value.artifact_revision], ['Artifact issues', value.artifact_issues.length]];
  $('#session-overview').replaceChildren(...overview.map(([term, description]) => { const wrap = document.createElement('div'); wrap.append(textElement('dt', term), textElement('dd', String(description))); return wrap; }));
  $('#capture-status').textContent = captureLabel(value); renderIssues('#capture-issues', value.capture.issues);
  const capability = value.transcription_execution; $('#transcription-capability').textContent = capability.available ? `Backend: ${capability.backend} · Model: ${capability.model} · Status: Ready` : `Transcription unavailable: ${capability.issue}`;
  $('#segments').replaceChildren(...value.session.segments.filter(segment => segment.status === 'stopped').map(segmentCard));
  if (!value.session.segments.some(segment => segment.status === 'stopped')) $('#segments').append(empty('No finalized segments yet. Pause or stop recording to finalize a segment.'));
  const sessionNote = value.artifacts.find(item => item.type === 'note' && item.scope.kind === 'session');
  $('#notes-status').textContent = sessionNote ? `Session notes created: ${sessionNote.relative_path} · ${sessionNote.related_transcript_artifact_ids?.length || 0} linked transcripts` : `Session notes are missing. Module notes: ${value.notes.module_exists ? 'created' : 'missing'}.`;
  renderArtifacts(value.artifacts); renderIssues('#artifact-issues', value.artifact_issues); updateControls(value);
}

function captureLabel(value) {
  const status = value.session.capture_status; const current = value.session.current_segment; const finalized = value.session.segments.filter(item => item.status === 'stopped').length;
  if (value.capture.issues.some(issue => issue.severity === 'error')) return 'Recovery required';
  if (status === 'recording') return `Recording segment ${String(current).padStart(3, '0')}`;
  if (status === 'paused') return `Paused after segment ${String(finalized).padStart(3, '0')}`;
  if (status === 'stopped') return 'Stopped';
  return 'Not recording';
}

function segmentCard(segment) {
  const card = textElement('article', '', 'segment'); const heading = textElement('h4', `Segment ${String(segment.number).padStart(3, '0')}`);
  card.append(heading, textElement('p', `Capture: Finalized · ${formatDuration(segment.duration_millis)} · ${formatBytes(segment.audio_size_bytes)}`), textElement('p', `Transcription: ${friendlyCode(segment.transcription_status)}${segment.queue_status ? ` · Queue: ${friendlyCode(segment.queue_status)}` : ''}`));
  if (segment.max_attempts) card.append(textElement('p', `Attempt ${segment.attempt}/${segment.max_attempts}${segment.language ? ` · Language ${segment.language}` : ''}`));
  const paths = [segment.transcript_json_relative_path, segment.transcript_text_relative_path, segment.provenance_relative_path, segment.job_metadata_relative_path].filter(Boolean);
  if (paths.length) card.append(textElement('p', `Artifacts: ${paths.join(' · ')}`, 'segment-paths'));
  if (segment.last_error_code) card.append(textElement('p', `Safe error: ${segment.last_error_code}`, 'warning-panel'));
  const button = document.createElement('button'); button.type = 'button'; button.textContent = state.busy.has(`transcribe:${segment.id}`) ? 'Transcribing…' : 'Transcribe'; button.disabled = !segment.can_transcribe || !state.workspace.transcription_execution.available || state.busy.has(`transcribe:${segment.id}`); button.title = segment.transcription_reason || (!state.workspace.transcription_execution.available ? state.workspace.transcription_execution.issue : ''); button.addEventListener('click', () => transcribe(segment.id)); card.append(button);
  return card;
}

function updateControls(value) {
  const mapping = {'start-session':'start_session','complete-session':'complete_session','start-capture':'start_capture','pause-capture':'pause_capture','resume-capture':'resume_capture','stop-capture':'stop_capture','create-session-notes':'create_session_notes','refresh-artifacts':'refresh_artifacts','inspect-artifacts':'inspect_artifacts'};
  for (const button of document.querySelectorAll('[data-action]')) {
    const key = mapping[button.dataset.action]; const pending = state.busy.has(button.dataset.action); button.disabled = pending || !value.controls[key]; button.setAttribute('aria-disabled', String(button.disabled)); button.title = value.control_reasons[key] || (button.dataset.action === 'create-session-notes' && value.notes.session_exists ? 'Session notes already exist.' : '');
  }
}

async function mutate(action, suffix, body, confirmation = null) {
  if (state.busy.has(action)) return;
  state.busy.add(action); if (state.workspace) updateControls(state.workspace);
  if (confirmation && !(await confirmAction(confirmation.title, confirmation.message))) { state.busy.delete(action); if (state.workspace) updateControls(state.workspace); return; }
  clearError(); announce(`${friendlyCode(action)} in progress…`);
  try { await api(`/sessions/${encode(state.course)}/${encode(state.module)}/${encode(state.session)}${suffix}`, {method:'POST', body:JSON.stringify(body)}); announce(`${friendlyCode(action)} completed.`); }
  catch (error) { showError(error); announce(`${friendlyCode(action)} did not complete.`); }
  finally { state.busy.delete(action); await loadSession(); }
}

async function transcribe(segmentID) {
  const key = `transcribe:${segmentID}`; if (state.busy.has(key)) return;
  state.busy.add(key); renderSession(state.workspace);
  if (!(await confirmAction('Start transcription?', 'Transcription runs locally and this browser request remains open until it completes, fails, times out, or is cancelled.'))) { state.busy.delete(key); renderSession(state.workspace); return; }
  const config = state.workspace.transcription_execution; const revision = state.workspace.session.revision; const controller = new AbortController(); state.transcriptionController = controller; clearError();
  $('#transcription-progress').hidden = false; $('#transcription-progress').textContent = 'Preparing transcription…'; $('#cancel-transcription').hidden = false; renderSession(state.workspace);
  window.addEventListener('beforeunload', warnDuringTranscription);
  try {
    $('#transcription-progress').textContent = 'Running transcription…';
    const result = await api(`/sessions/${encode(state.course)}/${encode(state.module)}/${encode(state.session)}/transcription/execute`, {method:'POST', signal:controller.signal, body:JSON.stringify({segment_id:segmentID, backend:config.backend, model:config.model, language:'en', max_attempts:3, expected_revision:revision})});
    $('#transcription-progress').textContent = `Completed · ${result.word_count} words · ${formatDuration(result.duration_millis)} · Runtime revision ${result.runtime_revision}`; announce('Transcription completed and authoritative state was refreshed.');
  } catch (error) {
    if (error.name === 'AbortError') showError({code:'cancelled', message:'The browser cancelled the transcription request.', recoverable:true}, 'Inspect authoritative state before retrying.'); else showError(error, 'Inspect the segment state before retrying manually.');
    $('#transcription-progress').textContent = error.name === 'AbortError' ? 'Cancelled' : 'Failed';
  } finally {
    window.removeEventListener('beforeunload', warnDuringTranscription); state.transcriptionController = null; state.busy.delete(key); $('#cancel-transcription').hidden = true; await loadSession();
  }
}

function warnDuringTranscription(event) { event.preventDefault(); event.returnValue = ''; }
async function refreshArtifacts() {
  const action = 'refresh-artifacts'; if (state.busy.has(action)) return; state.busy.add(action); setLoading('#artifact-loading', true); clearError();
  try { await api(`/courses/${encode(state.course)}/modules/${encode(state.module)}/artifacts/refresh`, {method:'POST', body:JSON.stringify({expected_artifact_revision:state.workspace.artifact_revision})}); announce('Artifact index refreshed.'); }
  catch (error) { showError(error); announce('Artifact index was not refreshed.'); }
  finally { state.busy.delete(action); setLoading('#artifact-loading', false); await loadSession(); }
}
async function inspectArtifacts() {
  if (state.busy.has('inspect-artifacts')) return; state.busy.add('inspect-artifacts'); setLoading('#artifact-loading', true); clearError();
  try { const value = await api(`/courses/${encode(state.course)}/modules/${encode(state.module)}/artifacts/inspect`); renderArtifacts(value.artifacts); renderIssues('#artifact-issues', value.issues); announce(`Artifact inspection completed with ${value.issues.length} issues.`); }
  catch (error) { showError(error); announce('Artifact inspection failed.'); }
  finally { state.busy.delete('inspect-artifacts'); setLoading('#artifact-loading', false); }
}
function renderArtifacts(artifacts) {
  const container = $('#artifacts'); container.replaceChildren();
  for (const artifact of artifacts) container.append(textElement('p', `${friendlyCode(artifact.type)} · ${artifact.title} · ${artifact.scope.kind} · ${artifact.relative_path} · ${formatBytes(artifact.size_bytes)} · SHA-256 ${artifact.sha256.slice(0, 12)}…`));
  if (!artifacts.length) container.append(empty('No indexed session artifacts.'));
}
function renderIssues(selector, issues) {
  const container = $(selector); container.replaceChildren(); const groups = {error:[], warning:[], information:[]};
  for (const issue of issues || []) (groups[issue.severity] || groups.information).push(issue);
  for (const severity of ['error','warning','information']) if (groups[severity].length) { const group = textElement('section', '', `issue-group issue-${severity}`); group.append(textElement('h4', friendlyCode(severity))); for (const issue of groups[severity]) group.append(textElement('p', `${issue.code || issue.kind}: ${issue.message}`)); container.append(group); }
  if (!(issues || []).length) container.append(empty('No diagnostic issues.'));
}
async function createModuleNotes() {
  if (state.busy.has('module-notes')) return; state.busy.add('module-notes'); $('#create-module-notes').disabled = true;
  try { await api(`/courses/${encode(state.course)}/modules/${encode(state.module)}/notes/module`, {method:'POST', body:JSON.stringify({title:'Module Notes', expected_artifact_revision:state.moduleWorkspace.artifact_revision})}); announce('Module notes created.'); }
  catch (error) { showError(error); }
  finally { state.busy.delete('module-notes'); await loadModule(state.course, state.module); }
}
function confirmAction(title, message) {
  const dialog = $('#confirmation-dialog'); $('#confirmation-title').textContent = title; $('#confirmation-message').textContent = message; dialog.showModal();
  return new Promise(resolve => dialog.addEventListener('close', () => resolve(dialog.returnValue === 'confirm'), {once:true}));
}
function formatDuration(milliseconds) { if (!milliseconds) return 'Duration unavailable'; const seconds = Math.round(milliseconds / 1000); return `${String(Math.floor(seconds / 60)).padStart(2,'0')}:${String(seconds % 60).padStart(2,'0')}`; }
function formatBytes(bytes) { if (!bytes) return '0 bytes'; if (bytes < 1024) return `${bytes} bytes`; return `${(bytes / 1024).toFixed(1)} KiB`; }

document.addEventListener('click', event => {
  const action = event.target.dataset?.action; if (!action || !state.workspace) return; const revision = state.workspace.session.revision;
  if (action === 'start-session') mutate(action, '/start', {expected_revision:revision});
  if (action === 'complete-session') mutate(action, '/complete', {expected_revision:revision}, {title:'Complete this session?', message:'Completion is explicit. Recording is not changed and must already be finalized.'});
  if (action === 'start-capture') mutate(action, '/capture/start', {expected_revision:revision});
  if (action === 'pause-capture') mutate(action, '/capture/pause', {expected_revision:revision});
  if (action === 'resume-capture') mutate(action, '/capture/resume', {expected_revision:revision});
  if (action === 'stop-capture') mutate(action, '/capture/stop', {expected_revision:revision}, {title:'Stop recording?', message:'Stop recording and finalize the current segment? The session will remain active.'});
  if (action === 'create-session-notes') mutate(action, '/notes/session', {title:'Session Notes', expected_artifact_revision:state.workspace.artifact_revision});
  if (action === 'refresh-artifacts') refreshArtifacts();
  if (action === 'inspect-artifacts') inspectArtifacts();
});
$('#create-session-form').addEventListener('submit', createSession); $('#create-module-notes').addEventListener('click', createModuleNotes);
$('#cancel-transcription').addEventListener('click', () => state.transcriptionController?.abort());
$('#courses-nav').addEventListener('click', () => { location.hash = '#/'; }); $('#back-to-courses').addEventListener('click', () => { location.hash = '#/'; }); $('#module-back').addEventListener('click', () => { location.hash = '#/'; });
$('#module-nav').addEventListener('click', () => { location.hash = `#/course/${encode(state.course)}/module/${encode(state.module)}`; }); $('#session-back').addEventListener('click', () => { location.hash = `#/course/${encode(state.course)}/module/${encode(state.module)}`; });
window.addEventListener('hashchange', () => route()); route();
