// MongoDB single-node initialization script for docker-compose.yml.

var appPassword = process.env.TRIBEWAY_MONGODB_APP_PASSWORD;

if (!appPassword) {
    throw new Error("TRIBEWAY_MONGODB_APP_PASSWORD is required");
}

var gameDB = db.getSiblingDB("tribeway_game");

try {
    gameDB.createUser({
        user: "tribeway_user",
        pwd: appPassword,
        roles: [
            { role: "readWrite", db: "tribeway_game" }
        ]
    });
    print("Created MongoDB application user tribeway_user");
} catch (e) {
    if (String(e).indexOf("already exists") >= 0 || e.codeName === "DuplicateKey") {
        print("MongoDB application user tribeway_user already exists");
    } else {
        throw e;
    }
}

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

print("MongoDB single-node initialization completed");
