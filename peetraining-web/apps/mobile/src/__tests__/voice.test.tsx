import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import { AudioModule } from 'expo-audio';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import { PracticeVoiceQueue, type VoiceJob } from '../features/practice/voiceQueue';
import { VoiceRecorder } from '../features/practice/VoiceRecorder';
import { SubjectiveFlow } from '../features/practice/SubjectiveFlow';
import { PaperPhotoQueue } from '../features/paper/photos';
import { storage, setJSON } from '../lib/storage';

jest.mock('expo-router', () => ({ router: { push: jest.fn(), replace: jest.fn(), back: jest.fn() } }));
jest.mock('../lib/flags', () => ({ useFeatureFlag: () => true }));
let mockUuid = 0;
jest.mock('expo-crypto', () => ({ randomUUID: () => `voice-${++mockUuid}` }));
const mockUpload = jest.fn(async () => ({ status: 200 }));
jest.mock('expo-file-system', () => ({
 Paths: { document: { uri: 'file:///documents/' } },
 Directory: class { uri: string; constructor(_root: unknown, name: string) { this.uri = `file:///documents/${name}/`; } create() {} },
 File: class {
  size = 2048; exists = true; uri: string;
  constructor(base: string | { uri: string }, name?: string) { this.uri = typeof base === 'string' ? base : `${base.uri}${name ?? ''}`; }
  copy() {} delete() {} upload = () => mockUpload();
 },
}));
const mockCalls: { method: string; path: string; init?: unknown }[] = [];
let mockResponses: Record<string, (init?: unknown) => unknown>;
async function mockRespond(method: string, path: string, init?: unknown) {
 mockCalls.push({ method, path, init });
 const r = await (mockResponses[`${method} ${path}`]?.(init) ?? { status: 200, data: {} }) as { status: number; data?: unknown; error?: unknown };
 return { ...r, response: new Response(null, { status: r.status }) };
}
jest.mock('../lib/api', () => {
 const m = (method: string) => (path: string, init?: unknown) => mockRespond(method,path,init);
 return { api: { GET: m('GET'), POST: m('POST'), DELETE: m('DELETE') }, unwrap: jest.requireActual('@training/api-client').unwrap };
});
const clip = { uri: 'file:///recording.m4a', mimeType: 'audio/m4a' as const, size: 2048 };
const job = (over: Partial<VoiceJob> = {}): VoiceJob => ({ id: 1, capture_order: 10, idempotency_key: 'voice-first', object_key: 'u/1/voice/1.m4a', status: 'ready', text: '第一段原话', ...over });
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>((done) => { resolve = done; }); return { promise, resolve }; }
function wrap(ui: React.ReactElement) {
 const qc = new QueryClient({defaultOptions:{queries:{retry:false,gcTime:Infinity}}});
 return render(<SafeAreaProvider initialMetrics={{ frame:{x:0,y:0,width:390,height:844},insets:{top:0,left:0,right:0,bottom:0} }}><QueryClientProvider client={qc}>{ui}</QueryClientProvider></SafeAreaProvider>);
}
beforeEach(() => {
 storage.clearAll(); mockCalls.length=0; mockUpload.mockReset(); mockUpload.mockResolvedValue({status:200});
 mockResponses={
  'POST /voice/upload-requests':()=>({status:200,data:{object_key:'u/1/voice/1.m4a',upload_url:'https://oss/voice',upload_headers:{}}}),
  'POST /practice-sessions/{sessionId}/questions/{questionId}/voice':(init)=>{const b=(init as {body:{idempotency_key:string;capture_order:number}}).body;return {status:200,data:job({idempotency_key:b.idempotency_key,capture_order:b.capture_order,status:'pending'})};},
  'GET /quota':()=>({status:200,data:{items:[]}}),
  'GET /purchase-entry':()=>({status:200,data:{enabled:false}}),
 };
});
it('录音上传未结束即可离开，队列按原题保存；重建后沿用录音和顺序',async()=>{
 const held=deferred<{status:number}>();mockUpload.mockReturnValueOnce(held.promise);
 const q=new PracticeVoiceQueue(900,201);const key=q.add(clip);
 expect(q.snapshot()[key]?.clip.uri).toContain('/documents/practice-voice/');
 const order=q.snapshot()[key]!.order;
 expect(q.snapshot()[key]?.status).toBe('uploading');
 held.resolve({status:200});await q.waitForUploads();
 expect(mockCalls.at(-1)?.init).toMatchObject({params:{path:{sessionId:900,questionId:201}},body:{idempotency_key:key,capture_order:order}});
 const restored=new PracticeVoiceQueue(900,201);await restored.waitForUploads();
 expect(restored.snapshot()[key]?.status).toBe('registered');expect(mockUpload).toHaveBeenCalledTimes(1);
});
it('登记响应丢失后用同一标识恢复，不重复上传录音',async()=>{
 let online=false;
 const register=mockResponses['POST /practice-sessions/{sessionId}/questions/{questionId}/voice']!;
 mockResponses['POST /practice-sessions/{sessionId}/questions/{questionId}/voice']=(init)=>{if(!online)throw new Error('响应丢失');return register(init);};
 const q=new PracticeVoiceQueue(901,201);const key=q.add(clip);
 await expect(q.waitForUploads()).rejects.toThrow('录音尚未上传');
 online=true;await q.run(key);await q.waitForUploads();
 expect(mockUpload).toHaveBeenCalledTimes(1);
 expect(mockCalls.filter(c=>c.path.endsWith('/voice')).map(c=>c.init)).toEqual([expect.objectContaining({body:expect.objectContaining({idempotency_key:key})}),expect.objectContaining({body:expect.objectContaining({idempotency_key:key})})]);
});
it('整卷录音登记复用后台处理接口，并保留 voice 类型',async()=>{
 mockResponses['POST /paper-sessions/{sessionId}/items/{seq}/handwriting']=(init)=>({status:200,data:{...job(),seq:(init as {params:{path:{seq:number}}}).params.path.seq,input_kind:'voice',object_keys:['u/1/voice/1.m4a'],low_confidence:[]}});
 const q=new PaperPhotoQueue(902);q.addVoice(2,clip);await q.waitForUploads();
 expect(mockCalls.at(-1)?.init).toMatchObject({params:{path:{sessionId:902,seq:2}},body:{input_kind:'voice'}});
 expect(mockCalls.some(c=>c.path==='/handwriting/recognize')).toBe(false);
});
it('录音结束且入队之后才允许切题；卸载也会先保存再释放录音器',async()=>{
 storage.set('permission_explained_microphone',true);
 const capture=jest.fn();const changed=jest.fn();const r=await wrap(<VoiceRecorder onCapture={capture} onRecordingChange={changed}/>);
 await fireEvent.press(screen.getByText('语音输入'));await screen.findByText('结束录音 0:00');
 const recorder=(AudioModule.AudioRecorder as unknown as jest.Mock).mock.results.at(-1)!.value;
 const held=deferred<void>();recorder.stop.mockReturnValueOnce(held.promise);
 await r.unmount();expect(capture).not.toHaveBeenCalled();expect(recorder.release).not.toHaveBeenCalled();
 await act(async()=>{held.resolve();await held.promise;});
 expect(capture).toHaveBeenCalledWith(clip);expect(recorder.release).toHaveBeenCalledTimes(1);
});
it('练习提交等待识别；默认采用多段原话并且失败重试不重复拼接',async()=>{
 let ready=false;let submissions=0;
 mockResponses['GET /practice-sessions/{sessionId}/questions/{questionId}/voice']=()=>({status:200,data:{items:[job(),job({id:2,idempotency_key:'voice-second',capture_order:20,status:ready?'ready':'pending',text:ready?'第二段原话':''})]}});
 mockResponses['POST /practice-sessions/{sessionId}/gradings']=()=>{submissions++;return {status:503,error:{code:'AI_FAILED',message:'稍后重试'}};};
 setJSON('practice:voice-edits:903:201',{'voice-second':'用户核对的第二段'});
 const r=await wrap(<SubjectiveFlow sessionId={903} q={{id:201,qtype:'term',stem:'意境',source:'exam',origin_tags:[],knowledge_points:[],rubric_count:3}} onGraded={jest.fn()} onQueued={jest.fn()} onSelfAssess={jest.fn()}/>);
 await screen.findByText('语音正在后台识别');
 await fireEvent.changeText(screen.getByLabelText('你的答案'),'手动输入');
 await fireEvent.press(screen.getByText('提交批改'));expect(submissions).toBe(0);
 ready=true;await fireEvent.press(screen.getByText('提交批改'));
 await waitFor(()=>expect(submissions).toBe(1));
 const first=mockCalls.find(c=>c.path.endsWith('/gradings'));
 expect(first?.init).toMatchObject({body:{answer_text:'手动输入\n第一段原话\n用户核对的第二段',answer_mode:'typed'}});
 await fireEvent.press(await screen.findByText('重试'));await waitFor(()=>expect(submissions).toBe(2));
 expect(mockCalls.filter(c=>c.path.endsWith('/gradings'))[1]?.init).toMatchObject({body:{answer_text:'手动输入\n第一段原话\n用户核对的第二段'}});
 await r.unmount();
});
