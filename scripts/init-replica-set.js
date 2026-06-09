// MongoDB replica set initialization script.

sleep(5000);

var adminPassword = process.env.TRIBEWAY_MONGODB_PASSWORD || process.env.MONGO_INITDB_ROOT_PASSWORD;
var appPassword = process.env.TRIBEWAY_MONGODB_APP_PASSWORD;

if (!adminPassword) {
    throw new Error("TRIBEWAY_MONGODB_PASSWORD or MONGO_INITDB_ROOT_PASSWORD is required");
}

if (!appPassword) {
    throw new Error("TRIBEWAY_MONGODB_APP_PASSWORD is required");
}

print("Starting MongoDB replica set initialization...");

try {
    rs.status();
    print("Replica set already initialized");
} catch (e) {
    print("Initializing replica set config...");

    var config = {
        _id: "rs0",
        version: 1,
        members: [
            { _id: 0, host: "172.20.2.1:27017", priority: 3, votes: 1 },
            { _id: 1, host: "172.20.2.2:27017", priority: 2, votes: 1 },
            { _id: 2, host: "172.20.2.3:27017", priority: 1, votes: 1 }
        ]
    };

    var result = rs.initiate(config);
    print("Replica set init result:", JSON.stringify(result));

    if (result.ok) {
        print("Replica set initialized successfully");
        sleep(10000);

        print("Creating admin user...");
        var adminResult = db.getSiblingDB("admin").createUser({
            user: "admin",
            pwd: adminPassword,
            roles: [
                { role: "root", db: "admin" },
                { role: "clusterAdmin", db: "admin" }
            ]
        });
        print("Admin user create result:", JSON.stringify(adminResult));

        print("Creating application user...");
        var appResult = db.getSiblingDB("tribeway_game").createUser({
            user: "tribeway_user",
            pwd: appPassword,
            roles: [
                { role: "readWrite", db: "tribeway_game" }
            ]
        });
        print("Application user create result:", JSON.stringify(appResult));

        print("Creating base collections and indexes...");
        var gameDB = db.getSiblingDB("tribeway_game");

        gameDB.users.createIndex({ "user_id": 1 }, { unique: true });
        gameDB.users.createIndex({ "username": 1 }, { unique: true });
        gameDB.users.createIndex({ "email": 1 });

        gameDB.friends.createIndex({ "user_id": 1, "friend_id": 1 });
        gameDB.friends.createIndex({ "user_id": 1 });
        gameDB.friends.createIndex({ "friend_id": 1 });

        gameDB.mails.createIndex({ "mail_id": 1 });
        gameDB.mails.createIndex({ "to_user_id": 1 });
        gameDB.mails.createIndex({ "expire_at": 1 });

        gameDB.game_records.createIndex({ "game_id": 1 });
        gameDB.game_records.createIndex({ "room_id": 1 });
        gameDB.game_records.createIndex({ "players.user_id": 1 });
        gameDB.game_records.createIndex({ "created_at": -1 });

        rs.secondaryOk();
        print("MongoDB replica set initialization completed");
    } else {
        print("Replica set initialization failed:", JSON.stringify(result));
    }
}

print("\nCurrent replica set status:");
try {
    var status = rs.status();
    print("Replica set:", status.set);
    print("Members:", status.members.length);

    status.members.forEach(function(member) {
        print("Node", member._id, ":", member.name, "-", member.stateStr);
    });
} catch (e) {
    print("Unable to get replica set status:", e);
}

print("Replica set init script finished");
