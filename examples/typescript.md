# TypeScript

```ts
const response = await fetch("http://server-ip:8721/create", {
  method: "POST",
  headers: { "content-type": "application/json" },
  body: JSON.stringify({
    name: "ts-cell",
    dbtype: "pg",
    port: 3001,
    ram: "128mb",
    disk: "512mb",
  }),
});

console.log(response.status);
console.log(await response.json());
```
