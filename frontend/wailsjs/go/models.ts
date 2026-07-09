export namespace main {
	
	export class ConnectRequest {
	    host: string;
	    proxyAddr: string;
	    proxyUser: string;
	    proxyPass: string;
	    uid: number;
	    gid: number;
	    forceVersion: number;
	
	    static createFrom(source: any = {}) {
	        return new ConnectRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.host = source["host"];
	        this.proxyAddr = source["proxyAddr"];
	        this.proxyUser = source["proxyUser"];
	        this.proxyPass = source["proxyPass"];
	        this.uid = source["uid"];
	        this.gid = source["gid"];
	        this.forceVersion = source["forceVersion"];
	    }
	}
	export class ConnectResult {
	    success: boolean;
	    version: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new ConnectResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.version = source["version"];
	        this.error = source["error"];
	    }
	}
	export class ExportInfo {
	    dir: string;
	    groups: string[];
	
	    static createFrom(source: any = {}) {
	        return new ExportInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dir = source["dir"];
	        this.groups = source["groups"];
	    }
	}
	export class FileInfo {
	    name: string;
	    handle: string;
	    type: string;
	    mode: string;
	    uid: number;
	    gid: number;
	    size: number;
	    mtime: string;
	
	    static createFrom(source: any = {}) {
	        return new FileInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.handle = source["handle"];
	        this.type = source["type"];
	        this.mode = source["mode"];
	        this.uid = source["uid"];
	        this.gid = source["gid"];
	        this.size = source["size"];
	        this.mtime = source["mtime"];
	    }
	}
	export class MountResult {
	    success: boolean;
	    rootHandle: string;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new MountResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.rootHandle = source["rootHandle"];
	        this.error = source["error"];
	    }
	}
	export class PortmapEntry {
	    program: number;
	    version: number;
	    protocol: string;
	    port: number;
	
	    static createFrom(source: any = {}) {
	        return new PortmapEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.program = source["program"];
	        this.version = source["version"];
	        this.protocol = source["protocol"];
	        this.port = source["port"];
	    }
	}

}

