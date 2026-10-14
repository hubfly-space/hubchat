import {expect,it} from "vitest";
import {parseCSVHeader} from "./csv";
it("reads quoted import columns without splitting quoted commas",()=>{expect(parseCSVHeader('\uFEFF"Client, name","Email address","A ""quote"""\r\nAlice,alice@example.com')).toEqual(["Client, name","Email address",'A "quote"']);expect(()=>parseCSVHeader('"unfinished')).toThrow();});
