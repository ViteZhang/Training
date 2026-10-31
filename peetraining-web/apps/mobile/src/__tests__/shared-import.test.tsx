import { fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import { Linking, Platform } from 'react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import type { ResolvedSharePayload } from 'expo-sharing';
import { clearSharedReceipts, receiveSharedFiles, removeSharedFiles, sharedInbox } from '../features/import/sharedInbox';
import { WeChatImportGuide } from '../features/import/WeChatImportGuide';
import { storage } from '../lib/storage';

jest.mock('expo-router', () => ({ router: { push: jest.fn(), replace: jest.fn(), back: jest.fn() } }));
let mockUuid = 0;
let mockFileSize = 2048;
let mockTargetSize = 2048;
const mockSavedFiles = new Map<string, number>();
const mockDelete = jest.fn();
const mockCopy = jest.fn();
jest.mock('expo-crypto', () => ({ randomUUID: () => `shared-${++mockUuid}` }));
jest.mock('expo-file-system', () => ({
 Paths:{document:{uri:'file:///documents/'}},
 Directory:class {uri='file:///documents/shared-imports/';create(){}},
 File:class {
  uri:string;constructor(base:string|{uri:string},name?:string){this.uri=typeof base==='string'?base:`${base.uri}${name??''}`;}
  get exists(){return mockSavedFiles.has(this.uri);}
  get size(){return this.uri.startsWith('file:///documents/') ? mockSavedFiles.get(this.uri) ?? 0 : mockFileSize;}
  async copy(target:{uri:string}){await mockCopy(target);mockSavedFiles.set(target.uri,mockTargetSize);}
  delete(){mockDelete(this.uri);if(!mockSavedFiles.delete(this.uri))throw new Error('file does not exist');}
 },
}));
const payload=(over:Partial<ResolvedSharePayload>={}):ResolvedSharePayload=>({value:'content://weixin/1',shareType:'file',mimeType:'application/pdf',contentUri:'content://weixin/1',contentType:'file',contentSize:2048,contentMimeType:'application/pdf',originalName:'文学讲义.pdf',...over} as ResolvedSharePayload);
beforeEach(()=>{clearSharedReceipts();removeSharedFiles(sharedInbox().map(file=>file.key));storage.clearAll();mockFileSize=2048;mockTargetSize=2048;mockCopy.mockReset();mockSavedFiles.clear();mockDelete.mockReset();});
it('从微信的临时URI复制文件，登录和选课前持久保存；重复接收不新增',async()=>{
 expect(await receiveSharedFiles([payload()])).toEqual([]);
 const saved=sharedInbox()[0]!;
 expect(saved.uri).toContain('/documents/shared-imports/');expect(saved.sharedKey).toBe(saved.key);expect(saved.name).toBe('文学讲义.pdf');
 expect(storage.getString('import:shared-inbox')).toContain('文学讲义.pdf');
 await receiveSharedFiles([payload()]);expect(sharedInbox()).toHaveLength(1);expect(mockCopy).toHaveBeenCalledTimes(1);
 removeSharedFiles([saved.key]);expect(sharedInbox()).toEqual([]);
});
it('拒绝网页/聊天文字和真实超限文件，不信任分享中虚报的大小',async()=>{
 expect(await receiveSharedFiles([payload({contentUri:'https://example.com/file',contentType:'website'})])).toHaveLength(1);
 mockFileSize=51*1024*1024;
 expect((await receiveSharedFiles([payload({contentSize:1})]))[0]).toContain('超过 50 MB');expect(sharedInbox()).toHaveLength(0);expect(mockCopy).not.toHaveBeenCalled();
});
it('一批里单个文件复制失败，保留成功文件并支持再次接收补齐',async()=>{
 mockCopy.mockImplementationOnce(()=>{throw new Error('临时权限失效');});
 const second=payload({contentUri:'content://weixin/2',value:'content://weixin/2',originalName:'题目.pdf'});
 expect(await receiveSharedFiles([payload(),second])).toHaveLength(1);expect(sharedInbox()).toHaveLength(1);
 expect(await receiveSharedFiles([payload(),second])).toHaveLength(0);expect(sharedInbox()).toHaveLength(2);
});
it('App 提示微信操作路径，并提供保存到手机后的选择入口',async()=>{
 const pick=jest.fn();const open=jest.spyOn(Linking,'openURL').mockResolvedValue(true);
 const r=await render(<SafeAreaProvider initialMetrics={{frame:{x:0,y:0,width:390,height:844},insets:{top:0,left:0,right:0,bottom:0}}}><WeChatImportGuide pickFile={pick}/></SafeAreaProvider>);
 await fireEvent.press(screen.getByText('从微信导入'));
 expect(screen.getByText(/其他应用打开/)).toBeTruthy();expect(screen.getByText(/没找到本 App/)).toBeTruthy();
 await fireEvent.press(screen.getByText('打开微信，按步骤分享'));expect(open).toHaveBeenCalledWith('weixin://');
 await fireEvent.press(screen.getByText('已保存到手机，选择文件'));expect(pick).toHaveBeenCalledTimes(1);
 await r.unmount();open.mockRestore();
});


it('Android 同名文件分别从原始授权URI复制，不因解析器缓存同名而覆盖', async () => {
 const original = Platform.OS;
 Object.defineProperty(Platform, 'OS', { value: 'android', configurable: true });
 try {
  const first = payload({ contentUri: 'file:///cache/同名.pdf', originalName: '同名.pdf' });
  const second = payload({ value: 'content://weixin/2', contentUri: 'file:///cache/同名.pdf', originalName: '同名.pdf' });
  expect(await receiveSharedFiles([first, second])).toEqual([]);
  expect(sharedInbox()).toHaveLength(2);
  expect(sharedInbox()[0]?.uri).not.toBe(sharedInbox()[1]?.uri);
 } finally { Object.defineProperty(Platform, 'OS', { value: original, configurable: true }); }
});


it('部分接收失败后，已导入移除的文件不会被原生重试重复加回', async () => {
 const second = payload({ value: 'content://weixin/2', contentUri: 'content://weixin/2' });
 mockCopy.mockImplementationOnce(() => {}).mockImplementationOnce(() => { throw new Error('暂时无法复制'); });
 expect(await receiveSharedFiles([payload(), second], true)).toHaveLength(1);
 removeSharedFiles(sharedInbox().map((file) => file.key));
 expect(await receiveSharedFiles([payload(), second], true)).toEqual([]);
 expect(sharedInbox()).toHaveLength(1);
 expect(sharedInbox()[0]?.sourceUri).toBe('content://weixin/2');
});


it('等待中文文件复制完成才读取大小和持久化，不提前删除尚不存在的副本', async () => {
 let finish!: () => void;
 mockCopy.mockImplementationOnce(() => new Promise<void>((resolve) => { finish = resolve; }));
 const receiving = receiveSharedFiles([payload({ originalName: '北师大77 文学综合真题.docx' })]);
 await waitFor(() => expect(mockCopy).toHaveBeenCalledTimes(1));
 expect(sharedInbox()).toEqual([]);
 expect(mockDelete).not.toHaveBeenCalled();
 finish();
 expect(await receiving).toEqual([]);
 expect(sharedInbox()[0]).toMatchObject({ name: '北师大77 文学综合真题.docx', size: 2048 });
 expect(mockDelete).not.toHaveBeenCalled();
});

it('异步复制失败返回原始错误，允许重新分享重试', async () => {
 mockCopy.mockRejectedValueOnce(new Error('微信临时权限失效'));
 expect(await receiveSharedFiles([payload()])).toEqual(['微信临时权限失效']);
 expect(sharedInbox()).toEqual([]);
 expect(mockDelete).not.toHaveBeenCalled();
 expect(await receiveSharedFiles([payload()])).toEqual([]);
 expect(sharedInbox()).toHaveLength(1);
});

it('复制后检查真实大小，清理空文件与超限文件', async () => {
 for (const size of [0, 51 * 1024 * 1024]) {
  mockTargetSize = size;
  const errors = await receiveSharedFiles([payload()]);
  expect(errors[0]).toContain(size ? '超过 50 MB' : '文件为空');
  expect(sharedInbox()).toEqual([]);
  expect(mockSavedFiles.size).toBe(0);
 }
 expect(mockDelete).toHaveBeenCalledTimes(2);
});

it('清理失败不覆盖复制后校验的错误', async () => {
 mockTargetSize = 0;
 mockDelete.mockImplementationOnce(() => { throw new Error('无法删除'); });
 expect(await receiveSharedFiles([payload()])).toEqual(['文件为空或未下载完成']);
 expect(sharedInbox()).toEqual([]);
});

it('原生分享与深链并发接收同一文件只复制并保存一次', async () => {
 let finish!: () => void;
 mockCopy.mockImplementationOnce(() => new Promise<void>((resolve) => { finish = resolve; }));
 const native = receiveSharedFiles([payload()], true);
 await waitFor(() => expect(mockCopy).toHaveBeenCalledTimes(1));
 const link = receiveSharedFiles([payload()]);
 finish();
 expect(await Promise.all([native, link])).toEqual([[], []]);
 expect(mockCopy).toHaveBeenCalledTimes(1);
 expect(sharedInbox()).toHaveLength(1);
});


it('异步复制中断后清理残留副本，保留复制失败原因', async () => {
 mockCopy.mockImplementationOnce(async (target: { uri: string }) => {
  mockSavedFiles.set(target.uri, 512);
  throw new Error('复制中断');
 });
 expect(await receiveSharedFiles([payload()])).toEqual(['复制中断']);
 expect(sharedInbox()).toEqual([]);
 expect(mockSavedFiles.size).toBe(0);
 expect(mockDelete).toHaveBeenCalledTimes(1);
});
