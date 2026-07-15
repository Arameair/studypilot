const state = { dashboard: null, course: null, module: null, session: null, busy: false };
const $ = (selector) => document.querySelector(selector);
const status = (message, error = false) => { const node = $('#status'); node.textContent = message; node.dataset.error = String(error); };

async function api(path, options = {}) {
  const response = await fetch(`/api/v1${path}`, { headers: options.body ? {'Content-Type': 'application/json'} : {}, ...options });
  const value = await response.json();
  if (!response.ok) {
    if (response.status === 409 && state.session) await loadSession();
    throw new Error(value.error?.message || 'StudyPilot request failed.');
  }
  return value;
}

function show(name) {
  for (const id of ['dashboard', 'module-view', 'session-view']) $(`#${id}`).hidden = id !== name;
}

async function loadDashboard() {
  show('dashboard'); status('Loading dashboard…');
  const value = await api('/dashboard'); state.dashboard = value;
  $('#dashboard-summary').textContent = `${value.courses} courses · ${value.modules} modules · ${value.artifact_issues} artifact issues`;
  $('#courses').replaceChildren(...value.course_modules.map(moduleButton));
  $('#unfinished-sessions').replaceChildren(...value.unfinished_sessions.map(sessionButton));
  $('#pending-transcripts').replaceChildren(...rows(value.pending_transcripts, 'No pending transcription work.'));
  $('#failed-transcripts').replaceChildren(...rows(value.failed_transcripts, 'No transcription failures.'));
  status('Dashboard is current.');
}

function moduleButton(module) {
  const button = document.createElement('button');
  button.textContent = `${module.number}. ${module.name} (${module.sessions} sessions)`;
  button.onclick = () => loadModule(module.course_id, module.id, module.name);
  return button;
}
function sessionButton(session) {
  const button = document.createElement('button');
  button.textContent = `${session.title} — ${session.session_status}`;
  button.onclick = () => loadSession(session.course_id, session.module_id, session.id);
  return button;
}
function rows(values, empty) {
  if (!values.length) { const p = document.createElement('p'); p.textContent = empty; return [p]; }
  return values.map(value => { const p = document.createElement('p'); p.textContent = `Segment ${String(value.segment_number).padStart(3,'0')}: ${value.status}`; return p; });
}

async function loadModule(course, module, name = '') {
  state.course = course; state.module = module; show('module-view'); status('Loading module…');
  const [value, inventory, inspection] = await Promise.all([
    api(`/courses/${encodeURIComponent(course)}/modules/${encodeURIComponent(module)}/sessions`),
    api(`/courses/${encodeURIComponent(course)}/modules/${encodeURIComponent(module)}/artifacts`),
    api(`/courses/${encodeURIComponent(course)}/modules/${encodeURIComponent(module)}/artifacts/inspect`)
  ]);
  $('#module-title').textContent = name || 'Module';
  const notes = inventory.artifacts.filter(item => item.type === 'note' && item.scope.kind === 'module').length;
  const assets = inventory.artifacts.filter(item => item.type === 'asset' && item.scope.kind === 'module').length;
  const transcripts = inventory.artifacts.filter(item => item.type === 'transcript').length;
  $('#module-summary').textContent = `${value.sessions.length} sessions · Module notes: ${notes ? 'created' : 'not created'} · ${assets} module assets · ${transcripts} transcripts · ${inspection.issues.length} artifact issues`;
  $('#module-sessions').replaceChildren(...value.sessions.map(sessionButton)); status('Module is current.');
}

async function loadSession(course = state.course, module = state.module, session = state.session) {
  state.course = course; state.module = module; state.session = session; show('session-view'); status('Loading session…');
  const value = await api(`/sessions/${encodeURIComponent(course)}/${encodeURIComponent(module)}/${encodeURIComponent(session)}`);
  state.workspace = value; $('#revision').textContent = `Revision ${value.session.revision}`;
  $('#session-title').textContent = value.session.title;
  $('#session-identity').textContent = `${value.course.name} · ${value.module.name} · Session ${value.session.number}`;
  $('#session-status').textContent = `Session: ${value.session.session_status} · Capture: ${value.session.capture_status} · Transcription: ${value.session.transcription_status}`;
  $('#capture-status').textContent = `Current segment: ${value.session.current_segment || 'none'}`;
  for (const button of document.querySelectorAll('[data-action]')) button.disabled = state.busy || !controlEnabled(button.dataset.action, value);
  $('#segments').replaceChildren(...value.session.segments.map(segmentRow));
  $('#artifacts').replaceChildren(...value.artifacts.map(artifactRow)); status('Session is current.');
}

function controlEnabled(action, value) {
  const controls = value.controls;
  return ({'start-session':controls.start_session,'start-capture':controls.start_capture,'pause-capture':controls.pause_capture,'resume-capture':controls.resume_capture,'stop-capture':controls.stop_capture,'complete-session':controls.complete_session,'create-session-notes':!value.notes.session_exists,'refresh-artifacts':true})[action];
}
function segmentRow(segment) {
  const row = document.createElement('div'); row.className = 'segment';
  const label = document.createElement('span');
  const attempt = segment.max_attempts ? ` · Attempt: ${segment.attempt}/${segment.max_attempts}` : '';
  const language = segment.language ? ` · Language: ${segment.language}` : '';
  const duration = segment.duration_millis ? ` · Duration: ${segment.duration_millis} ms` : '';
  const paths = segment.transcript_json_relative_path ? ` · Artifacts: ${segment.transcript_json_relative_path}, ${segment.transcript_text_relative_path}` : '';
  label.textContent = `Segment ${String(segment.number).padStart(3,'0')} · Capture: ${segment.status} · Transcription: ${segment.transcription_status}${attempt}${language}${duration}${paths}`;
  row.append(label);
  if (segment.can_transcribe && state.workspace.transcription_execution.available) { const button = document.createElement('button'); button.textContent = 'Transcribe'; button.onclick = () => transcribe(segment.id); row.append(button); }
  return row;
}
function artifactRow(artifact) { const p = document.createElement('p'); p.textContent = `${artifact.type}: ${artifact.title} · ${artifact.relative_path} · ${artifact.size_bytes} bytes`; return p; }

async function mutate(suffix, body) {
  state.busy = true; await loadSession();
  try { await api(`/sessions/${state.course}/${state.module}/${state.session}${suffix}`, {method:'POST', body:JSON.stringify(body)}); status('Operation completed.'); }
  catch (error) { status(error.message, true); }
  finally { state.busy = false; await loadSession(); }
}
async function transcribe(segmentID) {
  const {backend, model} = state.workspace.transcription_execution;
  await mutate('/transcription/execute', {segment_id:segmentID, backend, model, language:'en', max_attempts:3, expected_revision:state.workspace.session.revision});
}

document.addEventListener('click', event => {
  const action = event.target.dataset?.action; if (!action || !state.workspace) return;
  const revision = state.workspace.session.revision;
  const paths = {'start-session':'/start','complete-session':'/complete','start-capture':'/capture/start','pause-capture':'/capture/pause','resume-capture':'/capture/resume','stop-capture':'/capture/stop'};
  if (paths[action]) mutate(paths[action], {expected_revision:revision});
  if (action === 'create-session-notes') mutate('/notes/session', {title:'Session Notes', expected_artifact_revision:state.workspace.artifact_revision});
  if (action === 'refresh-artifacts') api(`/courses/${state.course}/modules/${state.module}/artifacts/refresh`, {method:'POST', body:JSON.stringify({expected_artifact_revision:state.workspace.artifact_revision})}).then(() => loadSession()).catch(error => status(error.message, true));
});
$('#dashboard-nav').onclick = () => loadDashboard().catch(error => status(error.message, true));
loadDashboard().catch(error => status(error.message, true));
