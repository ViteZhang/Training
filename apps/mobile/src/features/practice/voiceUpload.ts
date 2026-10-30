import { File } from 'expo-file-system';
import { api, unwrap } from '@/lib/api';
export type VoiceClip = { uri: string; mimeType: 'audio/m4a'; size: number };
export async function uploadVoice(clip: VoiceClip) {
  const target = await unwrap(api.POST('/voice/upload-requests', { body: { content_type: clip.mimeType, size: clip.size } }));
  const response = await new File(clip.uri).upload(target.upload_url, { httpMethod: 'PUT', headers: target.upload_headers });
  if (response.status < 200 || response.status >= 300) throw new Error('录音上传失败，请重试');
  return target.object_key;
}
