// MMKV 是原生模块，测试里换成内存实现。
jest.mock('react-native-mmkv', () => {
  const stores = new Map();
  return {
    createMMKV: ({ id } = { id: 'default' }) => {
      if (!stores.has(id)) stores.set(id, new Map());
      const m = stores.get(id);
      return {
        set: (k, v) => m.set(k, v),
        getString: (k) => m.get(k),
        getBoolean: (k) => m.get(k),
        remove: (k) => m.delete(k),
        clearAll: () => m.clear(),
      };
    },
  };
});

// 原生录音器在测试中用可控实例代替，业务队列和接口仍走实际代码。
jest.mock('expo-audio', () => ({
  AudioModule: { AudioRecorder: jest.fn().mockImplementation(() => ({
    uri: 'file:///recording.m4a', prepareToRecordAsync: jest.fn(async () => {}),
    record: jest.fn(), stop: jest.fn(async () => {}), release: jest.fn(),
  })) },
  RecordingPresets: { HIGH_QUALITY: { extension: '.m4a', sampleRate: 44100, numberOfChannels: 2, bitRate: 128000, ios: {}, android: {} } },
  requestRecordingPermissionsAsync: jest.fn(async () => ({ granted: true, canAskAgain: true })),
  setAudioModeAsync: jest.fn(async () => {}),
}));
