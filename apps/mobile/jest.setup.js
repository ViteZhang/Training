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
